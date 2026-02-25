package io.github.wharflab.tally.intellij.lsp

import com.intellij.openapi.vfs.VirtualFile

internal object TallyFileMatcher {
    fun isSupported(file: VirtualFile): Boolean = isSupportedName(file.name)

    fun isSupportedName(fileName: String): Boolean =
        fileName.startsWith("Dockerfile") ||
            fileName.startsWith("Containerfile") ||
            fileName.endsWith(".Dockerfile") ||
            fileName.endsWith(".Containerfile")
}
