package io.github.wharflab.tally.intellij.lsp

import com.intellij.openapi.components.Service
import com.intellij.openapi.components.SimplePersistentStateComponent
import com.intellij.openapi.components.State
import com.intellij.openapi.components.Storage
import com.intellij.openapi.components.service
import com.intellij.openapi.project.Project

@Service(Service.Level.PROJECT)
@State(name = "TallySettings", storages = [Storage("TallySettings.xml")])
class TallySettingsService(
    @Suppress("unused") private val project: Project,
) : SimplePersistentStateComponent<TallySettingsState>(TallySettingsState()) {
    val enabled: Boolean get() = state.enabled
    val executablePath: String? get() = state.executablePath
    val fixUnsafe: Boolean get() = state.fixUnsafe
    val fixAllOnSave: Boolean get() = state.fixAllOnSave
    val configurationPath: String? get() = state.configurationPath

    companion object {
        fun getInstance(project: Project): TallySettingsService = project.service()
    }
}
