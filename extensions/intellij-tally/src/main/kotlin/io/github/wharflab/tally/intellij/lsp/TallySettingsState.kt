package io.github.wharflab.tally.intellij.lsp

import com.intellij.openapi.components.BaseState

class TallySettingsState : BaseState() {
    var enabled by property(true)
    var executablePath by string()
    var fixUnsafe by property(false)
    var fixAllOnSave by property(false)
    var configurationPath by string()
}
