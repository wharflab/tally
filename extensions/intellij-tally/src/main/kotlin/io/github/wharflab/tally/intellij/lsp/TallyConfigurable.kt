package io.github.wharflab.tally.intellij.lsp

import com.intellij.openapi.diagnostic.Logger
import com.intellij.openapi.fileChooser.FileChooserDescriptorFactory
import com.intellij.openapi.options.BoundSearchableConfigurable
import com.intellij.openapi.project.Project
import com.intellij.ui.dsl.builder.COLUMNS_LARGE
import com.intellij.ui.dsl.builder.Cell
import com.intellij.ui.dsl.builder.bindSelected
import com.intellij.ui.dsl.builder.bindText
import com.intellij.ui.dsl.builder.columns
import com.intellij.ui.dsl.builder.panel
import javax.swing.JCheckBox

class TallyConfigurable(
    private val project: Project,
) : BoundSearchableConfigurable("Tally", "TallyConfigurable") {
    private val settings get() = TallySettingsService.getInstance(project)

    private companion object {
        private val LOG = Logger.getInstance(TallyConfigurable::class.java)
    }

    internal var enabledCheckBox: Cell<JCheckBox>? = null
        private set
    internal var fixAllOnSaveCheckBox: Cell<JCheckBox>? = null
        private set

    override fun createPanel() =
        panel {
            row {
                enabledCheckBox =
                    checkBox("Enable Tally")
                        .bindSelected(settings.state::enabled)
            }
            row("Executable path:") {
                textFieldWithBrowseButton(
                    FileChooserDescriptorFactory
                        .singleFile()
                        .withTitle("Select Tally Executable"),
                    project,
                ).bindText(
                    { settings.state.executablePath ?: "" },
                    { settings.state.executablePath = it.ifBlank { null } },
                ).columns(COLUMNS_LARGE)
            }
            row("Configuration file:") {
                textFieldWithBrowseButton(
                    FileChooserDescriptorFactory
                        .singleFile()
                        .withTitle("Select Tally Configuration File"),
                    project,
                ).bindText(
                    { settings.state.configurationPath ?: "" },
                    { settings.state.configurationPath = it.ifBlank { null } },
                ).columns(COLUMNS_LARGE)
            }
            row {
                checkBox("Allow unsafe fixes")
                    .bindSelected(settings.state::fixUnsafe)
            }
            row {
                checkBox("Format on reformat")
                    .bindSelected(settings.state::formatOnReformat)
                comment("Use Tally to format Dockerfiles on Reformat Code.")
            }
            row {
                fixAllOnSaveCheckBox =
                    checkBox("Fix all on save")
                        .bindSelected(settings.state::fixAllOnSave)
                comment("Apply safe fixes when saving Dockerfiles.")
            }
        }

    override fun apply() {
        super.apply()
        try {
            TallyServerService.getInstance(project).restartServer()
        } catch (e: Exception) {
            LOG.warn("Failed to restart Tally server on settings apply", e)
        }
    }
}
