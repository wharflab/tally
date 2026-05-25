package io.github.wharflab.tally.toolchain

import org.jetbrains.amper.plugins.Input
import org.jetbrains.amper.plugins.Output
import org.jetbrains.amper.plugins.TaskAction
import java.net.URI
import java.nio.file.attribute.PosixFilePermissions
import java.nio.file.Files
import java.nio.file.Path
import kotlin.io.path.createDirectories
import kotlin.io.path.exists
import kotlin.io.path.outputStream
import kotlin.io.path.setPosixFilePermissions

@TaskAction(executionAvoidance = org.jetbrains.amper.plugins.ExecutionAvoidance.Disabled)
fun runKtlint(
    ktlintVersion: String,
    @Input sourcesDir: Path,
    @Output cacheDir: Path,
    format: Boolean,
) {
    cacheDir.createDirectories()
    val binary = cacheDir.resolve("ktlint-$ktlintVersion")
    if (!binary.exists()) {
        val url = "https://github.com/pinterest/ktlint/releases/download/$ktlintVersion/ktlint"
        println("downloading $url")
        URI.create(url).toURL().openStream().use { input ->
            binary.outputStream().use { input.copyTo(it) }
        }
        runCatching {
            binary.setPosixFilePermissions(PosixFilePermissions.fromString("rwxr-xr-x"))
        }
    }
    val args = buildList {
        add(binary.toString())
        if (format) add("--format")
        add(sourcesDir.toString())
    }
    runProcess(*args.toTypedArray())
}
