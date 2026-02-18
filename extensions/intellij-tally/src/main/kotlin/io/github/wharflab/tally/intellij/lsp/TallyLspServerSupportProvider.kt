package io.github.wharflab.tally.intellij.lsp

import com.intellij.ide.trustedProjects.TrustedProjects
import com.intellij.openapi.project.Project
import com.intellij.openapi.roots.ProjectRootManager
import com.intellij.openapi.vfs.VirtualFile
import com.intellij.platform.lsp.api.LspServer
import com.intellij.platform.lsp.api.LspServerSupportProvider
import com.intellij.platform.lsp.api.lsWidget.LspServerWidgetItem

class TallyLspServerSupportProvider : LspServerSupportProvider {
    override fun fileOpened(
        project: Project,
        file: VirtualFile,
        serverStarter: LspServerSupportProvider.LspServerStarter,
    ) {
        if (!TallyFileMatcher.isSupported(file)) {
            return
        }

        val service = TallySettingsService.getInstance(project)
        if (!service.enabled) {
            return
        }

        val settings = TallySettings.fromService(service)
        val sdkHomePath = ProjectRootManager.getInstance(project).projectSdk?.homePath
        val command =
            TallyBinaryResolver.resolve(
                settings,
                project.basePath,
                sdkHomePath,
                TrustedProjects.isProjectTrusted(project),
            ) ?: return
        serverStarter.ensureServerStarted(
            TallyLspServerDescriptor(project, command, settings, service.formatOnReformat),
        )
    }

    override fun createLspServerWidgetItem(
        lspServer: LspServer,
        currentFile: VirtualFile?,
    ): LspServerWidgetItem = TallyWidgetItem(lspServer, currentFile)
}
