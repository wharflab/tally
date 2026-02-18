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

    fun fromService(service: TallySettingsService): TallyRuntimeSettings {
        val executablePaths =
            service.executablePath
                ?.takeIf { it.isNotBlank() }
                ?.let { listOf(it) }
                ?: emptyList()
        return TallyRuntimeSettings(
            executablePaths = executablePaths,
            importStrategy = IMPORT_STRATEGY_FROM_ENVIRONMENT,
            fixUnsafe = service.fixUnsafe,
            configurationOverride = service.configurationPath?.takeIf { it.isNotBlank() },
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
}
