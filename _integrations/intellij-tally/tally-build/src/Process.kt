package io.github.wharflab.tally.toolchain

import java.nio.file.Path

internal fun runProcess(vararg command: String) {
    val process = ProcessBuilder(*command)
        .redirectOutput(ProcessBuilder.Redirect.INHERIT)
        .redirectError(ProcessBuilder.Redirect.INHERIT)
        .start()
    val exit = process.waitFor()
    check(exit == 0) {
        "command failed (exit=$exit): ${command.joinToString(" ")}"
    }
}

/**
 * Find the actual IDE root inside an extracted distribution archive. The
 * tarball/zip wraps a single top-level directory (e.g. `WebStorm-252.26830.93`)
 * which is the real IDE home — we identify it by the presence of bin/, lib/,
 * and plugins/ subdirectories.
 */
internal fun locateIdeRoot(extractDir: Path): Path = extractDir.toFile()
    .walkTopDown()
    .maxDepth(6)
    .firstOrNull {
        it.isDirectory &&
            java.io.File(it, "bin").isDirectory &&
            java.io.File(it, "lib").isDirectory &&
            java.io.File(it, "plugins").isDirectory
    }
    ?.toPath()
    ?: error("unable to locate IDE home under $extractDir")
