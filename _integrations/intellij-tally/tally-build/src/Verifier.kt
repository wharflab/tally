package io.github.wharflab.tally.toolchain

import org.jetbrains.amper.plugins.Input
import org.jetbrains.amper.plugins.Output
import org.jetbrains.amper.plugins.TaskAction
import java.net.URI
import java.nio.file.Path
import kotlin.io.path.ExperimentalPathApi
import kotlin.io.path.createDirectories
import kotlin.io.path.deleteRecursively
import kotlin.io.path.exists
import kotlin.io.path.isDirectory
import kotlin.io.path.isRegularFile
import kotlin.io.path.outputStream
import kotlin.io.path.readLines
import kotlin.io.path.writeText

@OptIn(ExperimentalPathApi::class)
@TaskAction
fun runPluginVerifier(
    verifierUrl: String,
    @Input ideHome: Path,
    @Input pluginZipDir: Path,
    pluginVersion: String,
    enforceCommunityRules: Boolean,
    @Output reportsDir: Path,
) {
    // verifier-cli wipes its reports dir on start, so keep the log and
    // downloaded jar in the parent task directory.
    val taskRoot = reportsDir.parent
    if (reportsDir.exists()) reportsDir.deleteRecursively()
    reportsDir.createDirectories()

    val verifierJar = taskRoot.resolve("verifier.jar")
    if (!verifierJar.isRegularFile()) {
        println("downloading $verifierUrl")
        URI.create(verifierUrl).toURL().openStream().use { input ->
            verifierJar.outputStream().use { input.copyTo(it) }
        }
    }

    val pluginZip = pluginZipDir.resolve("tally-intellij-plugin-$pluginVersion.zip")
    check(pluginZip.isRegularFile()) { "plugin zip not found: $pluginZip" }

    val ide = locateIdeRoot(ideHome)
    val log = taskRoot.resolve("verifier.log")

    val process = ProcessBuilder(
        ProcessHandle.current().info().command().orElse("java"),
        "-jar", verifierJar.toString(),
        "check-plugin",
        "-verification-reports-dir", reportsDir.toString(),
        pluginZip.toString(),
        ide.toString(),
    ).redirectErrorStream(true).redirectOutput(log.toFile()).start()
    val exit = process.waitFor()

    if (enforceCommunityRules) {
        enforceCommunityResult(log, exit)
        println("smoke check passed against IntelliJ IDEA Community Edition")
        println("details: $log")
    } else {
        check(exit == 0) {
            log.readLines().takeLast(120).forEach(System.err::println)
            "plugin verifier exited with code $exit"
        }
    }
}

private fun enforceCommunityResult(log: Path, verifierExitCode: Int) {
    val lines = log.readLines()

    fun fail(message: String): Nothing {
        lines.takeLast(120).forEach(System.err::println)
        error(message)
    }

    if (lines.none { it.contains("Scheduled verifications (1):") }) {
        fail("smoke check failed: plugin verifier did not schedule a CE verification")
    }
    if (lines.any { it.contains("Plugin is invalid") }) {
        fail("smoke check failed: plugin is invalid for verification")
    }
    if (lines.any { it.contains("missing mandatory dependency") }) {
        fail("smoke check failed: missing mandatory dependency")
    }

    val problemRe = Regex("""Compatibility problems \([1-9]""")
    val hasProblems = lines.any { problemRe.containsMatchIn(it) }
    val onlyOptional = lines.any { it.contains("caused by absence of optional dependency") }
    if (hasProblems && !onlyOptional) {
        fail("smoke check failed: unexpected compatibility issues were reported")
    }

    if (verifierExitCode != 0) {
        fail("smoke check failed: plugin verifier exited with code $verifierExitCode")
    }
}

