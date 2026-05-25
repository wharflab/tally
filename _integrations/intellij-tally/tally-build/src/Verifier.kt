package io.github.wharflab.tally.toolchain

import org.jetbrains.amper.plugins.Input
import org.jetbrains.amper.plugins.Output
import org.jetbrains.amper.plugins.TaskAction
import java.nio.file.Path
import kotlin.io.path.ExperimentalPathApi
import kotlin.io.path.createDirectories
import kotlin.io.path.deleteRecursively
import kotlin.io.path.exists
import kotlin.io.path.isRegularFile
import kotlin.io.path.readLines

@OptIn(ExperimentalPathApi::class)
@TaskAction
fun runPluginVerifier(
    verifierUrl: String,
    verifierSha256: String,
    @Input ideHome: Path,
    @Input pluginZipDir: Path,
    pluginVersion: String,
    enforceCommunityRules: Boolean,
    @Output reportsDir: Path,
) {
    // verifier-cli wipes its reports dir on start, so keep the log and
    // downloaded jar in the parent task directory. Naming the cached jar
    // after its SHA-256 invalidates it automatically when the URL/version
    // changes.
    val taskRoot = reportsDir.parent
    if (reportsDir.exists()) reportsDir.deleteRecursively()
    reportsDir.createDirectories()

    val verifierJar = taskRoot.resolve("verifier-${verifierSha256.take(12)}.jar")
    downloadWithSha(verifierUrl, verifierJar, verifierSha256)

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

    // Verifier summary lines look like `Compatibility problems (N) ...`. We
    // must distinguish "every reported problem is caused by the absence of
    // optional dependencies" (acceptable on Community Edition, where the LSP
    // module is absent) from "at least one non-optional problem exists" — the
    // former is fine, the latter is a real failure. Looking only for *any*
    // optional-marker line elsewhere in the log mistakenly accepts mixed
    // logs, so check every problem-summary line individually.
    val problemRe = Regex("""Compatibility problems? \([1-9]\d*\)""")
    val nonOptional = lines.filter { problemRe.containsMatchIn(it) }
        .filterNot { it.contains("caused by absence of optional dependency") }
    if (nonOptional.isNotEmpty()) {
        fail("smoke check failed: unexpected compatibility issues were reported")
    }

    if (verifierExitCode != 0) {
        fail("smoke check failed: plugin verifier exited with code $verifierExitCode")
    }
}

