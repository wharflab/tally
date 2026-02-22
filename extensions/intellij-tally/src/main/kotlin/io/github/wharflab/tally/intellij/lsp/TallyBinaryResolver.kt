package io.github.wharflab.tally.intellij.lsp

import com.google.gson.JsonParser
import com.intellij.execution.configurations.PathEnvironmentVariableUtil
import com.intellij.ide.plugins.PluginManager
import com.intellij.openapi.diagnostic.Logger
import com.intellij.openapi.util.SystemInfo
import com.intellij.util.text.VersionComparatorUtil
import java.io.IOException
import java.nio.file.Files
import java.nio.file.InvalidPathException
import java.nio.file.Path
import java.nio.file.Paths
import java.util.concurrent.TimeUnit
import kotlin.io.path.absolutePathString

internal data class TallyCommand(
    val executable: String,
    val args: List<String>,
)

internal object TallyBinaryResolver {
    private val LOG = Logger.getInstance(TallyBinaryResolver::class.java)
    private val SERVER_ARGS = listOf("lsp", "--stdio")
    private const val VERSION_CHECK_TIMEOUT_MS = 2000L

    fun resolve(
        settings: TallyRuntimeSettings,
        projectBasePath: String?,
        projectSdkHomePath: String?,
        isTrustedProject: Boolean,
    ): TallyCommand? {
        if (settings.importStrategy == TallySettings.IMPORT_STRATEGY_USE_BUNDLED) {
            resolveBundledBinary()?.let { return it }
            return null
        }

        resolveExplicitPaths(settings.executablePaths, projectBasePath, isTrustedProject)
            ?.let { return it }
        if (isTrustedProject) {
            compatibleOrNull(resolveFromPath())?.let { return it }
            compatibleOrNull(resolveFromInterpreterDirectory(projectSdkHomePath))?.let { return it }
            compatibleOrNull(resolveFromProjectVenv(projectBasePath))?.let { return it }
        }
        return resolveBundledBinary()
    }

    private fun resolveExplicitPaths(
        paths: List<String>,
        projectBasePath: String?,
        isTrustedProject: Boolean,
    ): TallyCommand? {
        for (raw in paths) {
            val candidate = expandToPath(raw, projectBasePath, isTrustedProject) ?: continue
            if (!isUsableBinary(candidate)) {
                continue
            }
            return asCommand(candidate)
        }
        return null
    }

    private fun resolveFromPath(): TallyCommand? {
        val binary = PathEnvironmentVariableUtil.findInPath("tally") ?: return null
        return asCommand(binary.toPath())
    }

    private fun resolveFromInterpreterDirectory(projectSdkHomePath: String?): TallyCommand? {
        if (projectSdkHomePath.isNullOrBlank()) {
            return null
        }
        val interpreterPath =
            try {
                Paths.get(projectSdkHomePath)
            } catch (_: InvalidPathException) {
                return null
            }
        val interpreterDirectory = interpreterPath.parent ?: return null
        val binary = findExecutableIgnoringExtension(interpreterDirectory, "tally") ?: return null
        return asCommand(binary)
    }

    private fun resolveFromProjectVenv(projectBasePath: String?): TallyCommand? {
        if (projectBasePath.isNullOrBlank()) {
            return null
        }
        val projectRoot =
            try {
                Paths.get(projectBasePath)
            } catch (_: InvalidPathException) {
                return null
            }
        for (directory in venvBinaryDirectories(projectRoot)) {
            val binary = findExecutableIgnoringExtension(directory, "tally") ?: continue
            return asCommand(binary)
        }
        return null
    }

    private fun venvBinaryDirectories(projectRoot: Path): List<Path> =
        if (SystemInfo.isWindows) {
            listOf(
                projectRoot.resolve(".venv").resolve("Scripts"),
                projectRoot.resolve("venv").resolve("Scripts"),
            )
        } else {
            listOf(
                projectRoot.resolve(".venv").resolve("bin"),
                projectRoot.resolve("venv").resolve("bin"),
            )
        }

    private fun findExecutableIgnoringExtension(
        directory: Path,
        executableName: String,
    ): Path? {
        if (!Files.isDirectory(directory)) {
            return null
        }
        try {
            Files.newDirectoryStream(directory).use { entries ->
                for (entry in entries) {
                    if (!Files.isRegularFile(entry)) {
                        continue
                    }
                    val fileName = entry.fileName.toString()
                    val candidateName = fileName.substringBeforeLast('.', fileName)
                    if (candidateName != executableName) {
                        continue
                    }
                    if (isUsableBinary(entry)) {
                        return entry
                    }
                }
            }
        } catch (_: IOException) {
            return null
        }
        return null
    }

    private fun resolveBundledBinary(): TallyCommand? {
        val descriptor =
            PluginManager.getPluginByClass(TallyBinaryResolver::class.java)
                ?: return null
        val binaryName = if (SystemInfo.isWindows) "tally.exe" else "tally"
        val candidate =
            descriptor.pluginPath
                .resolve("bin")
                .resolve(platformFolder())
                .resolve(normalizeArch())
                .resolve(binaryName)
        if (!isUsableBinary(candidate)) {
            return null
        }
        return asCommand(candidate)
    }

    private fun isUsableBinary(path: Path): Boolean {
        if (!Files.isRegularFile(path)) {
            return false
        }
        return SystemInfo.isWindows || Files.isExecutable(path)
    }

    private fun asCommand(path: Path): TallyCommand =
        TallyCommand(
            executable = path.absolutePathString(),
            args = SERVER_ARGS,
        )

    private fun compatibleOrNull(command: TallyCommand?): TallyCommand? = command?.takeIf { isCompatibleBinary(Paths.get(it.executable)) }

    private fun getMinCompatibleVersion(): String? {
        val descriptor =
            PluginManager.getPluginByClass(TallyBinaryResolver::class.java)
                ?: return null
        val version = descriptor.version
        if (version.isNullOrBlank() || version.contains("dev")) {
            return null // dev build – skip version gating
        }
        return version
    }

    private fun isCompatibleBinary(path: Path): Boolean {
        val minVersion = getMinCompatibleVersion() ?: return true
        return try {
            val process =
                ProcessBuilder(path.absolutePathString(), "version", "--json")
                    .redirectError(ProcessBuilder.Redirect.DISCARD)
                    .start()
            val stdout = process.inputStream.bufferedReader().use { it.readText() }
            val completed = process.waitFor(VERSION_CHECK_TIMEOUT_MS, TimeUnit.MILLISECONDS)
            if (!completed) {
                process.destroyForcibly()
                LOG.warn("Skipping $path: version check timed out")
                return false
            }
            if (process.exitValue() != 0) {
                LOG.warn("Skipping $path: version check exited with ${process.exitValue()}")
                return false
            }
            val json = JsonParser.parseString(stdout).asJsonObject
            val candidateVersion = json.get("version")?.asString
            if (candidateVersion.isNullOrBlank()) {
                LOG.warn("Skipping $path: no version in output")
                return false
            }
            if (VersionComparatorUtil.compare(candidateVersion, minVersion) < 0) {
                LOG.warn("Skipping $path: version $candidateVersion < required $minVersion")
                return false
            }
            true
        } catch (e: Exception) {
            LOG.warn("Skipping $path: version check failed: ${e.message}")
            false
        }
    }

    private fun expandToPath(
        raw: String,
        projectBasePath: String?,
        isTrustedProject: Boolean,
    ): Path? {
        val trimmed = raw.trim()
        if (trimmed == "~") {
            return Paths.get(System.getProperty("user.home")).toAbsolutePath()
        }
        if (trimmed.startsWith("~/") || trimmed.startsWith("~\\")) {
            val suffix = trimmed.substring(2)
            return Paths.get(System.getProperty("user.home"), suffix).toAbsolutePath()
        }

        val candidate =
            try {
                Paths.get(trimmed)
            } catch (_: InvalidPathException) {
                return null
            }
        if (candidate.isAbsolute) {
            return candidate
        }
        if (!isTrustedProject) {
            return null
        }
        if (!projectBasePath.isNullOrBlank()) {
            return Paths
                .get(projectBasePath)
                .resolve(candidate)
                .normalize()
                .toAbsolutePath()
        }
        return candidate.toAbsolutePath()
    }

    private fun platformFolder(): String =
        when {
            SystemInfo.isWindows -> "windows"
            SystemInfo.isMac -> "darwin"
            else -> "linux"
        }

    private fun normalizeArch(): String {
        val arch = System.getProperty("os.arch").lowercase()
        return when (arch) {
            "x86_64", "amd64" -> "x64"
            "aarch64", "arm64" -> "arm64"
            else -> arch
        }
    }
}
