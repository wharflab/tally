package io.github.wharflab.tally.toolchain

import org.apache.commons.compress.archivers.zip.ZipArchiveEntry
import org.apache.commons.compress.archivers.zip.ZipArchiveOutputStream
import org.jetbrains.amper.plugins.CompilationArtifact
import org.jetbrains.amper.plugins.Input
import org.jetbrains.amper.plugins.Output
import org.jetbrains.amper.plugins.TaskAction
import java.nio.file.FileSystems
import java.nio.file.Files
import java.nio.file.Path
import java.nio.file.StandardCopyOption
import java.nio.file.attribute.PosixFileAttributeView
import java.nio.file.attribute.PosixFilePermission
import kotlin.io.path.ExperimentalPathApi
import kotlin.io.path.PathWalkOption
import kotlin.io.path.copyToRecursively
import kotlin.io.path.createDirectories
import kotlin.io.path.deleteRecursively
import kotlin.io.path.exists
import kotlin.io.path.isDirectory
import kotlin.io.path.isRegularFile
import kotlin.io.path.outputStream
import kotlin.io.path.readText
import kotlin.io.path.relativeTo
import kotlin.io.path.walk
import kotlin.io.path.writeText

/**
 * Repackages the module's compiled jar (which already contains classes plus
 * bundled resources) into the IntelliJ Marketplace-friendly layout:
 *
 *   dist/tally-intellij-plugin-<version>.zip
 *   └── <pluginName>/
 *       ├── lib/tally-intellij-plugin.jar    # the same jar with templated plugin.xml
 *       └── bin/...                          # optional bundled binaries (if intellij-plugin/bundled/bin exists)
 *
 * The plugin.xml inside the jar gets its @PLUGIN_ID@ / @PLUGIN_VERSION@ /
 * @SINCE_BUILD@ / @UNTIL_BUILD@ placeholders substituted in the process.
 */
@OptIn(ExperimentalPathApi::class)
@TaskAction
fun packagePlugin(
    @Input moduleJar: CompilationArtifact,
    @Input bundledDir: Path,
    pluginId: String,
    pluginName: String,
    pluginVersion: String,
    sinceBuild: String,
    untilBuild: String,
    @Output stagingDir: Path,
    @Output distDir: Path,
) {
    if (stagingDir.exists()) stagingDir.deleteRecursively()
    val pluginDir = stagingDir.resolve(pluginName)
    pluginDir.resolve("lib").createDirectories()

    val outputJar = pluginDir.resolve("lib/tally-intellij-plugin.jar")
    repackageJarWithTemplatedPluginXml(
        sourceJar = moduleJar.artifact,
        targetJar = outputJar,
        pluginId = pluginId,
        pluginVersion = pluginVersion,
        sinceBuild = sinceBuild,
        untilBuild = untilBuild,
    )

    val bundledBin = bundledDir.resolve("bin")
    if (bundledBin.isDirectory()) {
        val targetBin = pluginDir.resolve("bin").also { it.createDirectories() }
        bundledBin.copyToRecursively(targetBin, followLinks = false, overwrite = true)
    }

    distDir.createDirectories()
    val zip = distDir.resolve("tally-intellij-plugin-$pluginVersion.zip")
    if (zip.exists()) Files.delete(zip)
    zipDirectory(stagingDir, zip)

    println("built plugin zip: $zip")
}

/**
 * Build a zip from [source] preserving Unix permission bits (the IntelliJ
 * Marketplace and most extractors honor the external-attributes field that
 * commons-compress writes; `java.util.zip.ZipOutputStream` does not). Files
 * with no POSIX view (Windows hosts) fall back to `0644`/`0755` based on
 * directory-vs-file.
 *
 * The archive is written through the seekable [Path] constructor, NOT an
 * `OutputStream`. That distinction is load-bearing: given a non-seekable
 * stream, commons-compress falls back to *streaming mode* and emits a data
 * descriptor (general-purpose bit 3) for every entry, zeroing the size/CRC
 * fields in each local file header. The JetBrains Marketplace upload + zip
 * signing pipeline rejects such archives ("The plugin archive file cannot be
 * extracted") even though central-directory readers (unzip, jar, the plugin
 * verifier) accept them. With a seekable target, commons-compress backfills
 * the real CRC/sizes into the local headers and writes a conventional archive
 * matching the previous `zip`-built layout.
 */
@OptIn(ExperimentalPathApi::class)
private fun zipDirectory(source: Path, target: Path) {
    ZipArchiveOutputStream(target).use { zout ->
        source.walk(PathWalkOption.INCLUDE_DIRECTORIES)
            .filter { it != source }
            .sortedBy { it.relativeTo(source).toString() }
            .forEach { path ->
                val rel = path.relativeTo(source).toString().replace(java.io.File.separatorChar, '/')
                val isDir = path.isDirectory()
                val entryName = if (isDir) "$rel/" else rel
                val entry = ZipArchiveEntry(entryName).apply {
                    unixMode = unixModeFor(path, isDir)
                    // Store (not deflate) empty directory entries, matching a
                    // conventional `zip` layout. Deflating zero-byte dirs only
                    // existed because streaming mode defaulted everything to
                    // DEFLATED.
                    if (isDir) method = ZipArchiveEntry.STORED
                }
                zout.putArchiveEntry(entry)
                if (!isDir) Files.newInputStream(path).use { it.copyTo(zout) }
                zout.closeArchiveEntry()
            }
    }
}

private fun unixModeFor(path: Path, isDir: Boolean): Int {
    val view = Files.getFileAttributeView(path, PosixFileAttributeView::class.java)
    if (view != null) {
        runCatching {
            return view.readAttributes().permissions().fold(0) { acc, p -> acc or POSIX_BIT[p]!! }
        }
    }
    return if (isDir) 0b111_101_101 else 0b110_100_100  // 0755 / 0644
}

private val POSIX_BIT: Map<PosixFilePermission, Int> = mapOf(
    PosixFilePermission.OWNER_READ to 0x100,
    PosixFilePermission.OWNER_WRITE to 0x080,
    PosixFilePermission.OWNER_EXECUTE to 0x040,
    PosixFilePermission.GROUP_READ to 0x020,
    PosixFilePermission.GROUP_WRITE to 0x010,
    PosixFilePermission.GROUP_EXECUTE to 0x008,
    PosixFilePermission.OTHERS_READ to 0x004,
    PosixFilePermission.OTHERS_WRITE to 0x002,
    PosixFilePermission.OTHERS_EXECUTE to 0x001,
)

/**
 * Copy [sourceJar] to [targetJar] verbatim, then patch the contained
 * META-INF/plugin.xml in place via the JDK's zipfs (no full unpack/repack).
 */
private fun repackageJarWithTemplatedPluginXml(
    sourceJar: Path,
    targetJar: Path,
    pluginId: String,
    pluginVersion: String,
    sinceBuild: String,
    untilBuild: String,
) {
    Files.copy(sourceJar, targetJar, StandardCopyOption.REPLACE_EXISTING)

    val uri = java.net.URI.create("jar:" + targetJar.toUri())
    FileSystems.newFileSystem(uri, emptyMap<String, Any>()).use { fs ->
        val pluginXml = fs.getPath("/META-INF/plugin.xml")
        check(pluginXml.isRegularFile()) {
            "expected META-INF/plugin.xml inside ${sourceJar.fileName}"
        }
        val templated = pluginXml.readText()
            .replace("@PLUGIN_ID@", pluginId)
            .replace("@PLUGIN_VERSION@", pluginVersion)
            .replace("@SINCE_BUILD@", sinceBuild)
            .let {
                if (untilBuild.isNotEmpty()) {
                    it.replace("@UNTIL_BUILD@", untilBuild)
                } else {
                    it.replace(""" until-build="@UNTIL_BUILD@"""", "")
                }
            }
        pluginXml.writeText(templated)
    }
}
