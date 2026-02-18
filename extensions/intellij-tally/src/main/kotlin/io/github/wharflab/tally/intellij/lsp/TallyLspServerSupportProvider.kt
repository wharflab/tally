package io.github.wharflab.tally.intellij.lsp

import com.intellij.ide.trustedProjects.TrustedProjects
import com.intellij.openapi.project.Project
import com.intellij.openapi.roots.ProjectRootManager
import com.intellij.openapi.vfs.VirtualFile
import com.intellij.platform.lsp.api.LspServerSupportProvider

class TallyLspServerSupportProvider : LspServerSupportProvider {
    override fun fileOpened(
        project: Project,
        file: VirtualFile,
        serverStarter: LspServerSupportProvider.LspServerStarter,
    ) {
        if (!TallyFileMatcher.isSupported(file)) {
            return
        }

        val settings = TallySettings.current()
        val sdkHomePath = ProjectRootManager.getInstance(project).projectSdk?.homePath
        val command =
            TallyBinaryResolver.resolve(
                settings,
                project.basePath,
                sdkHomePath,
                TrustedProjects.isProjectTrusted(project),
            ) ?: return
        serverStarter.ensureServerStarted(TallyLspServerDescriptor(project, command, settings))
    }
}
