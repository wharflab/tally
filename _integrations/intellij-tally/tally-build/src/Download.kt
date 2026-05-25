package io.github.wharflab.tally.toolchain

import org.apache.commons.compress.archivers.tar.TarArchiveEntry
import org.apache.commons.compress.archivers.tar.TarArchiveInputStream
import org.apache.commons.compress.compressors.gzip.GzipCompressorInputStream
import org.jetbrains.amper.plugins.Output
import org.jetbrains.amper.plugins.TaskAction
import java.net.URI
import java.nio.file.Files
import java.nio.file.Path
import java.nio.file.StandardCopyOption
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

private const val CONNECT_TIMEOUT_MS = 30_000
private const val READ_TIMEOUT_MS = 600_000

@TaskAction
fun downloadAndExtract(
    url: String,
    sha256: String,
    @Output destination: Path,
) {
    require(sha256.isNotBlank()) { "sha256 must be pinned for $url" }

    val flag = destination.resolve(".source")
    val cookie = "$url\n$sha256"
    if (destination.isDirectory() && flag.exists() && flag.readText() == cookie) {
        return
    }

    val cacheDir = destination.parent.resolve("downloads").also { it.createDirectories() }
    val archive = cacheDir.resolve(url.substringAfterLast('/'))

    downloadWithSha(url, archive, sha256)

    @OptIn(ExperimentalPathApi::class)
    if (destination.exists()) destination.deleteRecursively()
    destination.createDirectories()

    when {
        url.endsWith(".tar.gz") || url.endsWith(".tgz") -> extractTarGz(archive, destination)
        url.endsWith(".zip") -> extractZip(archive, destination)
        else -> error("unsupported archive format: $url")
    }

    flag.writeText(cookie)
}

/**
 * Download [url] to [target] with a pinned [sha256]. Writes to a sibling
 * `.tmp` file and atomically renames into place on success — interrupted or
 * checksum-mismatched downloads never poison the cache. If [target] already
 * exists with the matching digest, returns without redownloading.
 */
internal fun downloadWithSha(url: String, target: Path, sha256: String) {
    require(sha256.isNotBlank()) { "sha256 must be pinned for $url" }

    if (target.exists() && sha256(target).equals(sha256, ignoreCase = true)) {
        return
    }
    if (target.exists()) Files.delete(target)

    target.parent.createDirectories()
    val tmp = target.resolveSibling(target.fileName.toString() + ".tmp")
    Files.deleteIfExists(tmp)

    println("downloading $url")
    try {
        val conn = URI.create(url).toURL().openConnection().apply {
            connectTimeout = CONNECT_TIMEOUT_MS
            readTimeout = READ_TIMEOUT_MS
        }
        conn.getInputStream().use { input ->
            tmp.outputStream().use { input.copyTo(it) }
        }
        val actual = sha256(tmp)
        check(actual.equals(sha256, ignoreCase = true)) {
            Files.deleteIfExists(tmp)
            "checksum mismatch for $url: expected $sha256, got $actual"
        }
        Files.move(tmp, target, StandardCopyOption.ATOMIC_MOVE, StandardCopyOption.REPLACE_EXISTING)
    } finally {
        Files.deleteIfExists(tmp)
    }
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
            // Reject symlinks whose link target escapes the destination root —
            // a malicious archive could otherwise plant a `bin -> ../../outside`
            // link and have later entries write through it.
            val resolvedLink = target.parent.resolve(entry.linkName).normalize()
            check(resolvedLink.startsWith(destination)) {
                "tar link escapes destination: ${entry.name} -> ${entry.linkName}"
            }
            target.parent.createDirectories()
            if (target.exists()) Files.delete(target)
            // Windows requires admin privileges (or Developer Mode) to create
            // symlinks; the IDE archive contains a few but the verifier doesn't
            // need them, so warn and continue rather than failing the build.
            runCatching {
                Files.createSymbolicLink(target, Path.of(entry.linkName))
            }.onFailure {
                System.err.println("warning: failed to create symlink $target -> ${entry.linkName}: ${it.message}")
            }
        }
        else -> {
            target.parent.createDirectories()
            target.outputStream().use { tin.copyTo(it) }
            val mode = entry.mode and 0x1FF
            if (mode != 0) {
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

internal fun sha256(path: Path): String {
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
