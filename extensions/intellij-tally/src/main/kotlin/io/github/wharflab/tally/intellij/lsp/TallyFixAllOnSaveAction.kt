package io.github.wharflab.tally.intellij.lsp

import com.intellij.ide.actionsOnSave.impl.ActionsOnSaveFileDocumentManagerListener
import com.intellij.notification.NotificationGroupManager
import com.intellij.notification.NotificationType
import com.intellij.openapi.editor.Document
import com.intellij.openapi.fileEditor.FileDocumentManager
import com.intellij.openapi.project.Project

class TallyFixAllOnSaveAction : ActionsOnSaveFileDocumentManagerListener.ActionOnSave() {
    override fun isEnabledForProject(project: Project): Boolean = TallySettingsService.getInstance(project).fixAllOnSave

    override fun processDocuments(
        project: Project,
        documents: Array<Document>,
    ) {
        val service = TallyServerService.getInstance(project)
        for (document in documents) {
            val file = FileDocumentManager.getInstance().getFile(document) ?: continue
            if (!TallyFileMatcher.isSupported(file)) continue
            try {
                service.fixAll(document)
            } catch (e: Exception) {
                NotificationGroupManager
                    .getInstance()
                    .getNotificationGroup("Tally")
                    .createNotification(
                        "Tally",
                        "Failed to apply fixes on save: ${e.message}",
                        NotificationType.WARNING,
                    ).notify(project)
            }
        }
    }
}
