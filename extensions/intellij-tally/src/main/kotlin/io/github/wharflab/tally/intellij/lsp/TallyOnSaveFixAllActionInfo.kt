package io.github.wharflab.tally.intellij.lsp

import com.intellij.ide.actionsOnSave.ActionOnSaveBackedByOwnConfigurable
import com.intellij.ide.actionsOnSave.ActionOnSaveContext

internal class TallyOnSaveFixAllActionInfo(
    context: ActionOnSaveContext,
) : ActionOnSaveBackedByOwnConfigurable<TallyConfigurable>(context, "TallyConfigurable", TallyConfigurable::class.java) {
    override fun getActionOnSaveName(): String = "Fix all Tally issues"

    override fun isApplicableAccordingToStoredState(): Boolean = true

    override fun isActionOnSaveEnabledAccordingToStoredState(): Boolean = TallySettingsService.getInstance(project).fixAllOnSave

    override fun setActionOnSaveEnabled(
        configurable: TallyConfigurable,
        enabled: Boolean,
    ) {
        configurable.fixAllOnSaveCheckBox?.component?.isSelected = enabled
    }
}
