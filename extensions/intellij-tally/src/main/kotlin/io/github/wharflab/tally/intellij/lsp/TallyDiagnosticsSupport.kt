package io.github.wharflab.tally.intellij.lsp

import com.intellij.platform.lsp.api.customization.LspDiagnosticsSupport
import org.eclipse.lsp4j.Diagnostic

internal class TallyDiagnosticsSupport : LspDiagnosticsSupport() {
    override fun getTooltip(diagnostic: Diagnostic): String {
        val code =
            diagnostic.code?.let { either ->
                when {
                    either.isLeft -> either.left
                    either.isRight -> either.right.toString()
                    else -> null
                }
            }
        val message = diagnostic.message
        val href = diagnostic.codeDescription?.href

        return if (code != null && href != null) {
            "tally(<a href=\"#tally-rule/$href\">$code</a>): $message"
        } else if (code != null) {
            "tally($code): $message"
        } else {
            "tally: $message"
        }
    }
}
