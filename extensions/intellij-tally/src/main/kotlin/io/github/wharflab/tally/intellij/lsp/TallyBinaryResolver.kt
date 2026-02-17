package io.github.wharflab.tally.intellij.lsp

import com.intellij.execution.configurations.PathEnvironmentVariableUtil
import com.intellij.ide.plugins.PluginUtils
import com.intellij.openapi.util.SystemInfo
import java.nio.file.Files
import java.nio.file.Path
import java.nio.file.Paths
import kotlin.io.path.absolutePathString

internal data class TallyCommand(
    val executable: String,
    val args: List<String>,
)

internal object TallyBinaryResolver {
    private val SERVER_ARGS = listOf("lsp", "--stdio")

    fun resolve(settings: TallyRuntimeSettings, projectBasePath: String?): TallyCommand? {
        if (settings.importStrategy == "useBundled") {
            resolveBundledBinary()?.let { return it }
            return null
        }

        resolveExplicitPaths(settings.executablePaths, projectBasePath)?.let { return it }
        resolveFromPath()?.let { return it }
        return resolveBundledBinary()
    }

    private fun resolveExplicitPaths(paths: List<String>, projectBasePath: String?): TallyCommand? {
        for (raw in paths) {
            val candidate = expandToPath(raw, projectBasePath)
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

    private fun resolveBundledBinary(): TallyCommand? {
        val descriptor = PluginUtils.getPluginDescriptorOrPlatformByClassName(
            TallyBinaryResolver::class.java.name,
        ) ?: return null
        val binaryName = if (SystemInfo.isWindows) "tally.exe" else "tally"
        val candidate = descriptor.pluginPath
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

    private fun asCommand(path: Path): TallyCommand {
        return TallyCommand(
            executable = path.absolutePathString(),
            args = SERVER_ARGS,
        )
    }

    private fun expandToPath(raw: String, projectBasePath: String?): Path {
        val trimmed = raw.trim()
        if (trimmed == "~") {
            return Paths.get(System.getProperty("user.home")).toAbsolutePath()
        }
        if (trimmed.startsWith("~/") || trimmed.startsWith("~\\")) {
            val suffix = trimmed.substring(2)
            return Paths.get(System.getProperty("user.home"), suffix).toAbsolutePath()
        }

        val candidate = Paths.get(trimmed)
        if (candidate.isAbsolute) {
            return candidate
        }
        if (!projectBasePath.isNullOrBlank()) {
            return Paths.get(projectBasePath).resolve(candidate).normalize().toAbsolutePath()
        }
        return candidate.toAbsolutePath()
    }

    private fun platformFolder(): String {
        return when {
            SystemInfo.isWindows -> "windows"
            SystemInfo.isMac -> "darwin"
            else -> "linux"
        }
    }

    private fun normalizeArch(): String {
        return when (System.getProperty("os.arch").lowercase()) {
            "x86_64", "amd64" -> "x64"
            "aarch64", "arm64" -> "arm64"
            else -> System.getProperty("os.arch").lowercase()
        }
    }
}
