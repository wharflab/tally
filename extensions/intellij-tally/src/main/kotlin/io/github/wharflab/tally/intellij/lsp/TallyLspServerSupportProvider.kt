package io.github.wharflab.tally.intellij.lsp

import com.intellij.openapi.project.Project
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
        val command = TallyBinaryResolver.resolve(settings) ?: return
        serverStarter.ensureServerStarted(TallyLspServerDescriptor(project, command, settings))
    }
}
