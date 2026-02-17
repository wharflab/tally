package io.github.wharflab.tally.intellij.lsp

import com.intellij.execution.configurations.GeneralCommandLine
import com.intellij.openapi.project.Project
import com.intellij.openapi.vfs.VirtualFile
import com.intellij.platform.lsp.api.ProjectWideLspServerDescriptor
import org.eclipse.lsp4j.ClientCapabilities
import org.eclipse.lsp4j.ConfigurationItem
import org.eclipse.lsp4j.WorkspaceClientCapabilities

internal class TallyLspServerDescriptor(
    project: Project,
    private val command: TallyCommand,
    private val settings: TallyRuntimeSettings,
) : ProjectWideLspServerDescriptor(project, "Tally") {
    override fun isSupportedFile(file: VirtualFile): Boolean = TallyFileMatcher.isSupported(file)

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
            val capabilities = super.clientCapabilities
            capabilities.workspace =
                (capabilities.workspace ?: WorkspaceClientCapabilities()).apply {
                    configuration = true
                }
            return capabilities
        }
}
