package io.github.wharflab.tally.toolchain

import org.jetbrains.amper.plugins.ExecutionAvoidance
import org.jetbrains.amper.plugins.Input
import org.jetbrains.amper.plugins.Output
import org.jetbrains.amper.plugins.TaskAction
import java.nio.file.Path
import kotlin.io.path.createDirectories

@TaskAction(executionAvoidance = ExecutionAvoidance.Disabled)
fun runKtlint(
    ktlintVersion: String,
    ktlintSha256: String,
    @Input sourcesDir: Path,
    @Output cacheDir: Path,
    format: Boolean,
) {
    cacheDir.createDirectories()
    // ktlint releases ship a self-executable jar (the leading bash shim is a
    // launcher), so `java -jar` works on every platform — no chmod, no
    // platform-specific exec bits, and no Windows-vs-shebang trouble.
    val binary = cacheDir.resolve("ktlint-$ktlintVersion.jar")
    val url = "https://github.com/pinterest/ktlint/releases/download/$ktlintVersion/ktlint"
    downloadWithSha(url, binary, ktlintSha256)

    val javaBin = ProcessHandle.current().info().command().orElse("java")
    val args = buildList {
        add(javaBin)
        add("-jar")
        add(binary.toString())
        if (format) add("--format")
        add(sourcesDir.toString())
    }
    runProcess(*args.toTypedArray())
}
