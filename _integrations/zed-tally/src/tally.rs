use zed_extension_api::{
    self as zed, register_extension,
    serde_json::{self, Value},
    settings::LspSettings,
    Architecture, Command, DownloadedFileType, Extension, GithubReleaseOptions, LanguageServerId,
    LanguageServerInstallationStatus, Os, Result, Worktree,
};

const SERVER_NAME: &str = "tally";

struct TallyExtension {
    cached_binary_path: Option<String>,
}

impl TallyExtension {
    /// Resolve the tally binary through a multi-step resolution chain:
    /// 1. User-configured binary path (Zed settings)
    /// 2. npm project-local (`package.json` depends on `tally-cli`)
    /// 3. Python venv (`.venv/bin/tally` or `venv/bin/tally`)
    /// 4. System PATH (`which tally`)
    /// 5. npm auto-install (platform-specific `@wharflab/tally-*` package)
    /// 6. GitHub release fallback
    fn resolve_binary(
        &mut self,
        language_server_id: &LanguageServerId,
        worktree: &Worktree,
    ) -> Result<String> {
        // 1. User-configured binary path
        if let Ok(lsp_settings) = LspSettings::for_worktree(SERVER_NAME, worktree) {
            if let Some(binary) = lsp_settings.binary {
                if let Some(path) = binary.path {
                    return Ok(path);
                }
            }
        }

        let root = worktree.root_path();

        // 2. npm project-local: check if package.json depends on tally-cli
        if let Some(path) = self.find_npm_binary(worktree, &root) {
            return Ok(path);
        }

        // 3. Python venv
        if let Some(path) = self.find_venv_binary(worktree, &root) {
            return Ok(path);
        }

        // 4. System PATH
        if let Some(path) = worktree.which("tally") {
            return Ok(path);
        }

        // 5 & 6. Auto-install: npm platform package, then GitHub release fallback
        self.ensure_installed(language_server_id)
    }

    /// Check if the project's package.json depends on `tally-cli` and return
    /// the path to the npm-installed binary.
    fn find_npm_binary(&self, worktree: &Worktree, root: &str) -> Option<String> {
        let package_json = worktree.read_text_file("package.json").ok()?;
        if has_tally_dependency(&package_json) {
            let path = format!("{root}/node_modules/.bin/tally");
            return Some(path);
        }
        None
    }

    /// Check for a Python venv containing tally.
    fn find_venv_binary(&self, worktree: &Worktree, root: &str) -> Option<String> {
        for venv_dir in [".venv", "venv"] {
            let cfg_path = format!("{venv_dir}/pyvenv.cfg");
            if worktree.read_text_file(&cfg_path).is_ok() {
                return Some(format!("{root}/{venv_dir}/bin/tally"));
            }
        }
        None
    }

    /// Install the tally binary via npm (platform-specific package) or GitHub
    /// release. Returns the path to the installed binary.
    fn ensure_installed(&mut self, id: &LanguageServerId) -> Result<String> {
        // Try npm platform-specific package first
        if let Ok(path) = self.ensure_installed_npm(id) {
            return Ok(path);
        }

        // Fall back to GitHub release
        self.ensure_installed_github(id)
    }

    /// Install or update via the npm platform-specific package (e.g.
    /// `@wharflab/tally-darwin-arm64`). Returns the path to the binary.
    fn ensure_installed_npm(&mut self, id: &LanguageServerId) -> Result<String> {
        let pkg = npm_package_name()?;

        let installed = zed::npm_package_installed_version(&pkg)?;
        let latest = zed::npm_package_latest_version(&pkg)?;

        if installed.as_deref() == Some(latest.as_str()) {
            let path = npm_binary_path(&pkg);
            self.cached_binary_path = Some(path.clone());
            return Ok(path);
        }

        zed::set_language_server_installation_status(
            id,
            &LanguageServerInstallationStatus::Downloading,
        );

        zed::npm_install_package(&pkg, &latest)?;

        let path = npm_binary_path(&pkg);
        zed::make_file_executable(&path)?;

        zed::set_language_server_installation_status(id, &LanguageServerInstallationStatus::None);

        self.cached_binary_path = Some(path.clone());
        Ok(path)
    }

    /// Install via GitHub release as a fallback. Returns the path to the binary.
    fn ensure_installed_github(&mut self, id: &LanguageServerId) -> Result<String> {
        let (platform, arch) = zed::current_platform();

        zed::set_language_server_installation_status(
            id,
            &LanguageServerInstallationStatus::Downloading,
        );

        let release = zed::latest_github_release(
            "wharflab/tally",
            GithubReleaseOptions {
                require_assets: true,
                pre_release: false,
            },
        )?;

        let version = &release.version;
        let (asset_name, file_type) = github_release_asset(&platform, &arch, version)?;

        let asset = release
            .assets
            .iter()
            .find(|a| a.name == asset_name)
            .ok_or_else(|| format!("no asset named {asset_name} in release {version}"))?;

        let version_dir = format!("tally-{}", release.version);
        let binary_path = format!("{version_dir}/tally");

        if !std::fs::metadata(&binary_path).is_ok_and(|m| m.is_file()) {
            zed::download_file(&asset.download_url, &version_dir, file_type)?;
            zed::make_file_executable(&binary_path)?;
        }

        zed::set_language_server_installation_status(id, &LanguageServerInstallationStatus::None);

        self.cached_binary_path = Some(binary_path.clone());
        Ok(binary_path)
    }
}

impl Extension for TallyExtension {
    fn new() -> Self {
        Self {
            cached_binary_path: None,
        }
    }

    fn language_server_command(
        &mut self,
        language_server_id: &LanguageServerId,
        worktree: &Worktree,
    ) -> Result<Command> {
        let binary_path = self.resolve_binary(language_server_id, worktree)?;

        // Allow users to override arguments via settings
        let args = if let Ok(lsp_settings) = LspSettings::for_worktree(SERVER_NAME, worktree) {
            lsp_settings
                .binary
                .and_then(|b| b.arguments)
                .unwrap_or_else(|| vec!["lsp".to_string(), "--stdio".to_string()])
        } else {
            vec!["lsp".to_string(), "--stdio".to_string()]
        };

        Ok(Command {
            command: binary_path,
            args,
            env: worktree.shell_env(),
        })
    }

    fn language_server_initialization_options(
        &mut self,
        _language_server_id: &LanguageServerId,
        worktree: &Worktree,
    ) -> Result<Option<Value>> {
        Ok(LspSettings::for_worktree(SERVER_NAME, worktree)
            .ok()
            .and_then(|s| s.initialization_options))
    }

    fn language_server_workspace_configuration(
        &mut self,
        _language_server_id: &LanguageServerId,
        worktree: &Worktree,
    ) -> Result<Option<Value>> {
        Ok(LspSettings::for_worktree(SERVER_NAME, worktree)
            .ok()
            .and_then(|s| s.settings))
    }
}

register_extension!(TallyExtension);

// ---------------------------------------------------------------------------
// Pure helper functions (testable without Zed API)
// ---------------------------------------------------------------------------

/// Map the current platform to the npm platform-specific package name.
fn npm_package_name() -> Result<String> {
    let (os, arch) = zed::current_platform();
    let os_str = match os {
        Os::Mac => "darwin",
        Os::Linux => "linux",
        Os::Windows => "windows",
    };
    let arch_str = match arch {
        Architecture::Aarch64 => "arm64",
        Architecture::X8664 => "x64",
        Architecture::X86 => return Err("x86 (32-bit) is not supported".into()),
    };
    Ok(format!("@wharflab/tally-{os_str}-{arch_str}"))
}

/// Build the path to the tally binary inside the npm platform-specific package.
fn npm_binary_path(package_name: &str) -> String {
    let binary_name = if cfg!(target_os = "windows") {
        "tally.exe"
    } else {
        "tally"
    };
    format!("node_modules/{package_name}/bin/{binary_name}")
}

/// Map `(Os, Architecture)` to the GitHub release asset name and file type.
fn github_release_asset(
    os: &Os,
    arch: &Architecture,
    version: &str,
) -> Result<(String, DownloadedFileType)> {
    let (os_str, file_type) = match os {
        Os::Mac => ("MacOS", DownloadedFileType::GzipTar),
        Os::Linux => ("Linux", DownloadedFileType::GzipTar),
        Os::Windows => ("Windows", DownloadedFileType::Zip),
    };
    let arch_str = match arch {
        Architecture::Aarch64 => "arm64",
        Architecture::X8664 => "x86_64",
        Architecture::X86 => return Err("x86 (32-bit) is not supported".into()),
    };
    let ext = match file_type {
        DownloadedFileType::GzipTar => "tar.gz",
        DownloadedFileType::Zip => "zip",
        _ => "tar.gz",
    };
    Ok((
        format!("tally_{version}_{os_str}_{arch_str}.{ext}"),
        file_type,
    ))
}

/// Check whether a `package.json` string contains `tally-cli` as a dependency.
fn has_tally_dependency(package_json: &str) -> bool {
    let Ok(parsed) = serde_json::from_str::<Value>(package_json) else {
        return false;
    };
    !parsed["dependencies"]["tally-cli"].is_null()
        || !parsed["devDependencies"]["tally-cli"].is_null()
}

// ---------------------------------------------------------------------------
// Tests (run with `cargo test` on native target, not WASM)
// ---------------------------------------------------------------------------

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_has_tally_dependency_in_deps() {
        let json = r#"{"dependencies": {"tally-cli": "^0.5.0"}}"#;
        assert!(has_tally_dependency(json));
    }

    #[test]
    fn test_has_tally_dependency_in_dev_deps() {
        let json = r#"{"devDependencies": {"tally-cli": "latest"}}"#;
        assert!(has_tally_dependency(json));
    }

    #[test]
    fn test_has_tally_dependency_absent() {
        let json = r#"{"dependencies": {"express": "^4.0.0"}}"#;
        assert!(!has_tally_dependency(json));
    }

    #[test]
    fn test_has_tally_dependency_empty() {
        assert!(!has_tally_dependency("{}"));
    }

    #[test]
    fn test_has_tally_dependency_invalid_json() {
        assert!(!has_tally_dependency("not json"));
    }

    #[test]
    fn test_npm_binary_path() {
        let path = npm_binary_path("@wharflab/tally-darwin-arm64");
        assert_eq!(path, "node_modules/@wharflab/tally-darwin-arm64/bin/tally");
    }
}
