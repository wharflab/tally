package io.github.wharflab.tally.intellij.lsp

import com.intellij.ide.actionsOnSave.ActionOnSaveContext
import com.intellij.ide.actionsOnSave.ActionOnSaveInfo
import com.intellij.ide.actionsOnSave.ActionOnSaveInfoProvider

internal class TallyOnSaveInfoProvider : ActionOnSaveInfoProvider() {
    override fun getActionOnSaveInfos(context: ActionOnSaveContext): List<ActionOnSaveInfo> = listOf(TallyOnSaveFixAllActionInfo(context))

    override fun getSearchableOptions(): Collection<String> = listOf("Fix all Tally issues")
}
