package io.github.wharflab.tally.intellij.lsp

import com.intellij.ide.trustedProjects.TrustedProjects
import com.intellij.openapi.extensions.PluginAware
import com.intellij.openapi.extensions.PluginDescriptor
import com.intellij.openapi.project.Project
import com.intellij.openapi.roots.ProjectRootManager
import com.intellij.openapi.vfs.VirtualFile
import com.intellij.platform.lsp.api.LspServer
import com.intellij.platform.lsp.api.LspServerSupportProvider
import com.intellij.platform.lsp.api.lsWidget.LspServerWidgetItem

internal class TallyLspServerSupportProvider :
    LspServerSupportProvider,
    PluginAware {
    // Injected by the extensions framework when this provider is loaded from
    // plugin.xml. This is the supported, public way for an extension to learn
    // its own install path and version (replaces PluginManager.getPluginByClass).
    private var pluginDescriptor: PluginDescriptor? = null

    override fun setPluginDescriptor(pluginDescriptor: PluginDescriptor) {
        this.pluginDescriptor = pluginDescriptor
    }

    override fun fileOpened(
        project: Project,
        file: VirtualFile,
        serverStarter: LspServerSupportProvider.LspServerStarter,
    ) {
        if (!TallyFileMatcher.isSupported(file)) {
            return
        }

        TallyServerService.getInstance(project)
        val service = TallySettingsService.getInstance(project)
        if (!service.enabled) {
            return
        }

        val isTrustedProject = TrustedProjects.isProjectTrusted(project)
        val settings = TallySettings.fromService(service, isTrustedProject)
        val sdkHomePath = ProjectRootManager.getInstance(project).projectSdk?.homePath
        val command =
            TallyBinaryResolver.resolve(
                settings,
                project.basePath,
                sdkHomePath,
                isTrustedProject,
                pluginDescriptor?.let {
                    TallyPluginInfo(
                        path = it.pluginPath,
                        version = it.version,
                    )
                },
            ) ?: return
        serverStarter.ensureServerStarted(
            TallyLspServerDescriptor(project, command, service.formatOnReformat),
        )
    }

    override fun createLspServerWidgetItem(
        lspServer: LspServer,
        currentFile: VirtualFile?,
    ): LspServerWidgetItem = TallyWidgetItem(lspServer, currentFile)
}
