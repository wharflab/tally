package io.github.wharflab.tally.intellij.lsp

import com.intellij.execution.configurations.GeneralCommandLine
import com.intellij.openapi.project.Project
import com.intellij.openapi.vfs.VirtualFile
import com.intellij.platform.lsp.api.ProjectWideLspServerDescriptor
import com.intellij.platform.lsp.api.customization.LspCustomization
import com.intellij.platform.lsp.api.customization.LspDiagnosticsCustomizer
import com.intellij.platform.lsp.api.customization.LspFormattingCustomizer
import com.intellij.platform.lsp.api.customization.LspFormattingDisabled
import org.eclipse.lsp4j.ClientCapabilities
import org.eclipse.lsp4j.ConfigurationItem
import org.eclipse.lsp4j.WorkspaceClientCapabilities

internal class TallyLspServerDescriptor(
    project: Project,
    private val command: TallyCommand,
    private val settings: TallyRuntimeSettings,
    private val formatOnReformat: Boolean,
) : ProjectWideLspServerDescriptor(project, "Tally") {
    override fun isSupportedFile(file: VirtualFile): Boolean = TallyFileMatcher.isSupported(file)

    override val lspCustomization: LspCustomization
        get() =
            object : LspCustomization() {
                override val diagnosticsCustomizer: LspDiagnosticsCustomizer
                    get() = TallyDiagnosticsSupport()

                override val formattingCustomizer: LspFormattingCustomizer
                    get() =
                        if (formatOnReformat) {
                            TallyFormattingSupport()
                        } else {
                            LspFormattingDisabled
                        }
            }

    override fun createCommandLine(): GeneralCommandLine {
        val commandLine = GeneralCommandLine(command.executable, *command.args.toTypedArray())
        project.basePath?.let { commandLine.withWorkDirectory(it) }
        return commandLine
    }

    override fun createInitializationOptions(): Any = TallySettings.initializationOptions(settings)

    override fun getWorkspaceConfiguration(item: ConfigurationItem): Any? {
        if (item.section != null && item.section != "tally") {
            return null
        }
        return TallySettings.workspaceConfiguration(settings)
    }

    override val clientCapabilities: ClientCapabilities
        get() {
            val base = super.clientCapabilities
            val workspaceCapabilities =
                WorkspaceClientCapabilities().apply {
                    base.workspace?.let { workspace ->
                        applyEdit = workspace.applyEdit
                        workspaceEdit = workspace.workspaceEdit
                        didChangeConfiguration = workspace.didChangeConfiguration
                        didChangeWatchedFiles = workspace.didChangeWatchedFiles
                        symbol = workspace.symbol
                        executeCommand = workspace.executeCommand
                        workspaceFolders = workspace.workspaceFolders
                        semanticTokens = workspace.semanticTokens
                        codeLens = workspace.codeLens
                        fileOperations = workspace.fileOperations
                        inlayHint = workspace.inlayHint
                        inlineValue = workspace.inlineValue
                        diagnostics = workspace.diagnostics
                    }
                    configuration = true
                }

            return ClientCapabilities().apply {
                workspace = workspaceCapabilities
                textDocument = base.textDocument
                notebookDocument = base.notebookDocument
                window = base.window
                general = base.general
                experimental = base.experimental
            }
        }
}
