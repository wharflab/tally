package io.github.wharflab.tally.intellij.lsp

internal data class TallyRuntimeSettings(
    val executablePaths: List<String>,
    val importStrategy: String,
    val fixUnsafe: Boolean,
    val configurationOverride: String?,
)

internal object TallySettings {
    internal const val IMPORT_STRATEGY_FROM_ENVIRONMENT = "fromEnvironment"
    internal const val IMPORT_STRATEGY_USE_BUNDLED = "useBundled"

    fun current(): TallyRuntimeSettings {
        val executablePaths = readExecutablePaths()
        val importStrategy = normalizeImportStrategy(System.getProperty("tally.importStrategy"))
        val fixUnsafe = System.getProperty("tally.fixUnsafe")?.toBoolean() ?: false
        val configurationOverride = System.getProperty("tally.configurationOverride")?.ifBlank { null }
        return TallyRuntimeSettings(
            executablePaths = executablePaths,
            importStrategy = importStrategy,
            fixUnsafe = fixUnsafe,
            configurationOverride = configurationOverride,
        )
    }

    fun initializationOptions(settings: TallyRuntimeSettings): Map<String, Any?> = mapOf("tally" to lspEnvelope(settings))

    fun workspaceConfiguration(settings: TallyRuntimeSettings): Map<String, Any?> = lspEnvelope(settings)

    private fun lspEnvelope(settings: TallyRuntimeSettings): Map<String, Any?> =
        mapOf(
            "version" to 1,
            "global" to
                mapOf(
                    "enable" to true,
                    "path" to settings.executablePaths,
                    "importStrategy" to settings.importStrategy,
                    "configuration" to settings.configurationOverride,
                    "configurationPreference" to "editorFirst",
                    "fixUnsafe" to settings.fixUnsafe,
                ),
            "workspaces" to emptyList<Map<String, Any?>>(),
        )

    private fun readExecutablePaths(): List<String> =
        System
            .getProperty("tally.executablePaths")
            ?.takeIf { it.isNotBlank() }
            ?.let(::splitList)
            ?: System
                .getProperty("tally.path")
                ?.takeIf { it.isNotBlank() }
                ?.let { listOf(it.trim()) }
            ?: System
                .getenv("TALLY_EXECUTABLE_PATHS")
                ?.takeIf { it.isNotBlank() }
                ?.let(::splitList)
            ?: emptyList()

    private fun splitList(raw: String): List<String> =
        raw
            .split(",")
            .map { it.trim() }
            .filter { it.isNotEmpty() }

    private fun normalizeImportStrategy(raw: String?): String =
        if (raw == IMPORT_STRATEGY_USE_BUNDLED) {
            IMPORT_STRATEGY_USE_BUNDLED
        } else {
            IMPORT_STRATEGY_FROM_ENVIRONMENT
        }
}
