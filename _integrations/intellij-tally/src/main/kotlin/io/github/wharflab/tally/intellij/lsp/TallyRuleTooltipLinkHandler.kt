package io.github.wharflab.tally.intellij.lsp

import com.intellij.codeInsight.highlighting.TooltipLinkHandler
import com.intellij.ide.BrowserUtil
import com.intellij.openapi.editor.Editor

internal class TallyRuleTooltipLinkHandler : TooltipLinkHandler() {
    override fun handleLink(
        refSuffix: String,
        editor: Editor,
    ): Boolean {
        if (refSuffix.isNotBlank()) {
            BrowserUtil.browse(refSuffix)
            return true
        }
        return false
    }
}
