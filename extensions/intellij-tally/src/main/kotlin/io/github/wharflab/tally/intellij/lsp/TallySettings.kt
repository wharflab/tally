package io.github.wharflab.tally.intellij.lsp

internal data class TallyRuntimeSettings(
    val executablePaths: List<String>,
    val importStrategy: String,
    val fixUnsafe: Boolean,
    val configurationOverride: String?,
)

internal object TallySettings {
    private const val IMPORT_STRATEGY_FROM_ENVIRONMENT = "fromEnvironment"
    private const val IMPORT_STRATEGY_USE_BUNDLED = "useBundled"

    fun current(): TallyRuntimeSettings {
        val executablePaths = readExecutablePaths()
        val importStrategy = normalizeImportStrategy(System.getProperty("tally.importStrategy"))
        val fixUnsafe = System.getProperty("tally.fixUnsafe")?.toBooleanStrictOrNull() ?: false
        val configurationOverride = System.getProperty("tally.configurationOverride")?.ifBlank { null }
        return TallyRuntimeSettings(
            executablePaths = executablePaths,
            importStrategy = importStrategy,
            fixUnsafe = fixUnsafe,
            configurationOverride = configurationOverride,
        )
    }

    fun initializationOptions(settings: TallyRuntimeSettings): Map<String, Any?> {
        return mapOf("tally" to lspEnvelope(settings))
    }

    fun workspaceConfiguration(settings: TallyRuntimeSettings): Map<String, Any?> {
        return lspEnvelope(settings)
    }

    private fun lspEnvelope(settings: TallyRuntimeSettings): Map<String, Any?> {
        return mapOf(
            "version" to 1,
            "global" to mapOf(
                "enable" to true,
                "path" to settings.executablePaths,
                "importStrategy" to settings.importStrategy,
                "configuration" to settings.configurationOverride,
                "configurationPreference" to "editorFirst",
                "fixUnsafe" to settings.fixUnsafe,
            ),
            "workspaces" to emptyList<Map<String, Any?>>(),
        )
    }

    private fun readExecutablePaths(): List<String> {
        val explicitPaths = System.getProperty("tally.executablePaths")
        if (!explicitPaths.isNullOrBlank()) {
            return splitList(explicitPaths)
        }

        val explicitSingle = System.getProperty("tally.path")
        if (!explicitSingle.isNullOrBlank()) {
            return listOf(explicitSingle.trim())
        }

        val envPaths = System.getenv("TALLY_EXECUTABLE_PATHS")
        if (!envPaths.isNullOrBlank()) {
            return splitList(envPaths)
        }

        return emptyList()
    }

    private fun splitList(raw: String): List<String> {
        return raw.split(",")
            .map { it.trim() }
            .filter { it.isNotEmpty() }
    }

    private fun normalizeImportStrategy(raw: String?): String {
        return if (raw == IMPORT_STRATEGY_USE_BUNDLED) {
            IMPORT_STRATEGY_USE_BUNDLED
        } else {
            IMPORT_STRATEGY_FROM_ENVIRONMENT
        }
    }
}
