package io.github.wharflab.tally.toolchain

import org.apache.commons.compress.archivers.tar.TarArchiveEntry
import org.apache.commons.compress.archivers.tar.TarArchiveInputStream
import org.apache.commons.compress.compressors.gzip.GzipCompressorInputStream
import org.jetbrains.amper.plugins.Output
import org.jetbrains.amper.plugins.TaskAction
import java.net.URI
import java.nio.file.Files
import java.nio.file.Path
import java.nio.file.attribute.PosixFilePermission
import java.security.MessageDigest
import java.util.zip.ZipInputStream
import kotlin.io.path.ExperimentalPathApi
import kotlin.io.path.createDirectories
import kotlin.io.path.deleteRecursively
import kotlin.io.path.exists
import kotlin.io.path.inputStream
import kotlin.io.path.isDirectory
import kotlin.io.path.outputStream
import kotlin.io.path.readText
import kotlin.io.path.writeText

@TaskAction
fun downloadAndExtract(
    url: String,
    sha256: String,
    @Output destination: Path,
) {
    val flag = destination.resolve(".source")
    val cookie = "$url\n$sha256"
    if (destination.isDirectory() && flag.exists() && flag.readText() == cookie) {
        return
    }

    val cacheDir = destination.parent.resolve("downloads").also { it.createDirectories() }
    val archiveName = url.substringAfterLast('/')
    val archive = cacheDir.resolve(archiveName)

    if (!archive.exists() || (sha256.isNotEmpty() && sha256(archive) != sha256)) {
        if (archive.exists()) Files.delete(archive)
        download(url, archive)
        if (sha256.isNotEmpty()) {
            val actual = sha256(archive)
            check(actual == sha256) {
                "checksum mismatch for $url: expected $sha256, got $actual"
            }
        }
    }

    @OptIn(ExperimentalPathApi::class)
    if (destination.exists()) destination.deleteRecursively()
    destination.createDirectories()

    when {
        archiveName.endsWith(".tar.gz") || archiveName.endsWith(".tgz") -> extractTarGz(archive, destination)
        archiveName.endsWith(".zip") -> extractZip(archive, destination)
        else -> error("unsupported archive format: $archiveName")
    }

    flag.writeText(cookie)
}

private fun extractTarGz(archive: Path, destination: Path) {
    TarArchiveInputStream(GzipCompressorInputStream(archive.inputStream().buffered())).use { tin ->
        var entry: TarArchiveEntry? = tin.nextEntry
        while (entry != null) {
            extractTarEntry(tin, entry, destination)
            entry = tin.nextEntry
        }
    }
}

private fun extractTarEntry(tin: TarArchiveInputStream, entry: TarArchiveEntry, destination: Path) {
    val target = destination.resolve(entry.name).normalize()
    check(target.startsWith(destination)) { "tar slip: ${entry.name}" }
    when {
        entry.isDirectory -> target.createDirectories()
        entry.isSymbolicLink -> {
            target.parent.createDirectories()
            if (target.exists()) Files.delete(target)
            Files.createSymbolicLink(target, Path.of(entry.linkName))
        }
        else -> {
            target.parent.createDirectories()
            target.outputStream().use { tin.copyTo(it) }
            val mode = entry.mode and 0x1FF
            if (mode != 0) {
                // Filesystem may not support POSIX permissions (Windows / FAT);
                // executable IDE binaries are not relevant in that case anyway.
                runCatching { Files.setPosixFilePermissions(target, posixFromMode(mode)) }
            }
        }
    }
}

private fun extractZip(archive: Path, destination: Path) {
    ZipInputStream(archive.inputStream().buffered()).use { zin ->
        while (true) {
            val entry = zin.nextEntry ?: break
            val target = destination.resolve(entry.name).normalize()
            check(target.startsWith(destination)) { "zip slip: ${entry.name}" }
            if (entry.isDirectory) {
                target.createDirectories()
            } else {
                target.parent.createDirectories()
                target.outputStream().use { zin.copyTo(it) }
            }
        }
    }
}

private val MODE_BITS = listOf(
    0x100 to PosixFilePermission.OWNER_READ,
    0x080 to PosixFilePermission.OWNER_WRITE,
    0x040 to PosixFilePermission.OWNER_EXECUTE,
    0x020 to PosixFilePermission.GROUP_READ,
    0x010 to PosixFilePermission.GROUP_WRITE,
    0x008 to PosixFilePermission.GROUP_EXECUTE,
    0x004 to PosixFilePermission.OTHERS_READ,
    0x002 to PosixFilePermission.OTHERS_WRITE,
    0x001 to PosixFilePermission.OTHERS_EXECUTE,
)

private fun posixFromMode(mode: Int): Set<PosixFilePermission> = buildSet {
    for ((bit, perm) in MODE_BITS) if (mode and bit != 0) add(perm)
}

private fun download(url: String, target: Path) {
    println("downloading $url")
    URI.create(url).toURL().openStream().use { input ->
        target.outputStream().use { input.copyTo(it) }
    }
}

private fun sha256(path: Path): String {
    val digest = MessageDigest.getInstance("SHA-256")
    path.inputStream().use { stream ->
        val buf = ByteArray(64 * 1024)
        while (true) {
            val n = stream.read(buf)
            if (n <= 0) break
            digest.update(buf, 0, n)
        }
    }
    return digest.digest().joinToString("") { "%02x".format(it) }
}
