package io.github.wharflab.tally.toolchain

import org.jetbrains.amper.plugins.CompilationArtifact
import org.jetbrains.amper.plugins.Input
import org.jetbrains.amper.plugins.Output
import org.jetbrains.amper.plugins.TaskAction
import java.nio.file.FileSystems
import java.nio.file.Files
import java.nio.file.Path
import java.nio.file.StandardCopyOption
import java.util.zip.ZipEntry
import java.util.zip.ZipOutputStream
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

@OptIn(ExperimentalPathApi::class)
private fun zipDirectory(source: Path, target: Path) {
    ZipOutputStream(target.outputStream().buffered()).use { zout ->
        source.walk(PathWalkOption.INCLUDE_DIRECTORIES)
            .filter { it != source }
            .sortedBy { it.relativeTo(source).toString() }
            .forEach { path ->
                val rel = path.relativeTo(source).toString().replace(java.io.File.separatorChar, '/')
                if (path.isDirectory()) {
                    zout.putNextEntry(ZipEntry("$rel/"))
                    zout.closeEntry()
                } else {
                    zout.putNextEntry(ZipEntry(rel))
                    Files.newInputStream(path).use { it.copyTo(zout) }
                    zout.closeEntry()
                }
            }
    }
}

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
