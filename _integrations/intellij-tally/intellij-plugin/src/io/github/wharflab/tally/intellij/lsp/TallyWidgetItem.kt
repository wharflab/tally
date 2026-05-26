package io.github.wharflab.tally.intellij.lsp

import com.intellij.openapi.vfs.VirtualFile
import com.intellij.platform.lsp.api.LspServer
import com.intellij.platform.lsp.api.lsWidget.LspServerWidgetItem

internal class TallyWidgetItem(
    lspServer: LspServer,
    currentFile: VirtualFile?,
) : LspServerWidgetItem(lspServer, currentFile, TallyIcons.Tally, TallyConfigurable::class.java) {
    override val serverLabel: String
        get() {
            val version = lspServer.initializeResult?.serverInfo?.version
            val base = if (version != null) "Tally $version" else lspServer.descriptor.presentableName
            return base + rootPostfix
        }
}
