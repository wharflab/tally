package io.github.wharflab.tally.toolchain

import org.jetbrains.amper.plugins.Configurable

@Configurable
interface TallyBuildSettings {
    val pluginId: String
    val pluginName: String
    val pluginVersion: String
    val pluginSinceBuild: String
    val pluginUntilBuild: String get() = ""

    /**
     * IntelliJ IDEA Community archive URL — used by both the plugin verifier
     * and the smoke check. Linux tarball is downloaded on every host because
     * the verifier doesn't care about the host platform.
     */
    val ideArchiveUrl: String
    val ideArchiveSha256: String

    val pluginVerifierUrl: String

    val ktlintVersion: String
}
