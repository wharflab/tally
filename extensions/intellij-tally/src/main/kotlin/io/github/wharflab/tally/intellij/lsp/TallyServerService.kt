package io.github.wharflab.tally.intellij.lsp

import com.intellij.openapi.command.WriteCommandAction
import com.intellij.openapi.components.service
import com.intellij.openapi.editor.Document
import com.intellij.openapi.fileEditor.FileDocumentManager
import com.intellij.openapi.project.Project
import com.intellij.platform.lsp.api.LspServerManager
import org.eclipse.lsp4j.CodeActionContext
import org.eclipse.lsp4j.CodeActionParams
import org.eclipse.lsp4j.Position
import org.eclipse.lsp4j.Range
import org.eclipse.lsp4j.TextEdit
import org.eclipse.lsp4j.WorkspaceEdit

class TallyServerService(
    private val project: Project,
) {
    fun restartServer() {
        LspServerManager
            .getInstance(project)
            .stopAndRestartIfNeeded(TallyLspServerSupportProvider::class.java)
    }

    fun fixAll(document: Document) {
        val file = FileDocumentManager.getInstance().getFile(document) ?: return
        val servers =
            LspServerManager
                .getInstance(project)
                .getServersForProvider(TallyLspServerSupportProvider::class.java)

        for (server in servers) {
            val textDocId = server.getDocumentIdentifier(file)

            val lastLine = maxOf(document.lineCount - 1, 0)
            val lastLineLength = document.getLineEndOffset(lastLine) - document.getLineStartOffset(lastLine)
            val params =
                CodeActionParams(
                    textDocId,
                    Range(Position(0, 0), Position(lastLine, lastLineLength)),
                    CodeActionContext(emptyList(), listOf("source.fixAll.tally")),
                )

            val result =
                try {
                    server.sendRequestSync(TIMEOUT_MS) { languageServer ->
                        languageServer.textDocumentService.codeAction(params)
                    }
                } catch (_: Exception) {
                    continue
                } ?: continue

            WriteCommandAction.runWriteCommandAction(project) {
                for (actionOrCommand in result) {
                    if (!actionOrCommand.isRight) continue
                    val action = actionOrCommand.right
                    val edit = action.edit ?: continue
                    applyWorkspaceEdit(document, edit, textDocId.uri)
                }
            }
        }
    }

    private fun applyWorkspaceEdit(
        document: Document,
        edit: WorkspaceEdit,
        fileUri: String,
    ) {
        val changes =
            edit.changes?.get(fileUri)
                ?: edit.documentChanges
                    ?.mapNotNull { it.left }
                    ?.find { it.textDocument.uri == fileUri }
                    ?.edits
                ?: return

        applyTextEdits(document, changes)
    }

    private fun applyTextEdits(
        document: Document,
        edits: List<TextEdit>,
    ) {
        val sortedEdits =
            edits.sortedWith(
                compareByDescending<TextEdit> { it.range.end.line }
                    .thenByDescending { it.range.end.character },
            )

        for (textEdit in sortedEdits) {
            val startOffset = getOffset(document, textEdit.range.start)
            val endOffset = getOffset(document, textEdit.range.end)
            if (startOffset in 0..endOffset && endOffset <= document.textLength) {
                document.replaceString(startOffset, endOffset, textEdit.newText)
            }
        }
    }

    private fun getOffset(
        document: Document,
        position: Position,
    ): Int {
        val line = position.line.coerceIn(0, maxOf(document.lineCount - 1, 0))
        val lineStartOffset = document.getLineStartOffset(line)
        val lineEndOffset = document.getLineEndOffset(line)
        val character = position.character.coerceIn(0, lineEndOffset - lineStartOffset)
        return lineStartOffset + character
    }

    companion object {
        private const val TIMEOUT_MS = 5000

        fun getInstance(project: Project): TallyServerService = project.service()
    }
}
