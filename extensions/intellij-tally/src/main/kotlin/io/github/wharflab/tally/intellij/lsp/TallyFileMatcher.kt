package io.github.wharflab.tally.intellij.lsp

import com.intellij.openapi.vfs.VirtualFile

internal object TallyFileMatcher {
    fun isSupported(file: VirtualFile): Boolean = isSupportedName(file.name)

    fun isSupportedName(fileName: String): Boolean {
        if (fileName.startsWith("Dockerfile") || fileName.startsWith("Containerfile")) {
            return true
        }
        return fileName.endsWith(".Dockerfile")
    }
}
