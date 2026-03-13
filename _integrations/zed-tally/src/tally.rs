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
        // 1. User-configured binary path (always re-check to pick up settings changes)
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

        // 5 & 6. Auto-install: return cached path from a previous install, or
        // run the full install flow (npm platform package → GitHub release).
        if let Some(ref path) = self.cached_binary_path {
            return Ok(path.clone());
        }
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
    /// Uses runtime OS detection since this code compiles to WASM.
    fn find_venv_binary(&self, worktree: &Worktree, root: &str) -> Option<String> {
        let (os, _) = zed::current_platform();
        let (subdir, binary_name) = match os {
            Os::Windows => ("Scripts", "tally.exe"),
            _ => ("bin", "tally"),
        };
        for venv_dir in [".venv", "venv"] {
            let cfg_path = format!("{venv_dir}/pyvenv.cfg");
            if worktree.read_text_file(&cfg_path).is_ok() {
                return Some(format!("{root}/{venv_dir}/{subdir}/{binary_name}"));
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
        let (os, _) = zed::current_platform();
        let pkg = npm_package_name()?;

        let installed = zed::npm_package_installed_version(&pkg)?;
        let latest = zed::npm_package_latest_version(&pkg)?;

        if installed.as_deref() == Some(latest.as_str()) {
            let path = npm_binary_path(&pkg, &os);
            self.cached_binary_path = Some(path.clone());
            return Ok(path);
        }

        zed::set_language_server_installation_status(
            id,
            &LanguageServerInstallationStatus::Downloading,
        );

        zed::npm_install_package(&pkg, &latest)?;

        let path = npm_binary_path(&pkg, &os);
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
        let binary_name = match platform {
            Os::Windows => "tally.exe",
            _ => "tally",
        };
        let binary_path = format!("{version_dir}/{binary_name}");

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
/// Uses runtime OS detection since this code compiles to WASM.
fn npm_binary_path(package_name: &str, os: &Os) -> String {
    let binary_name = match os {
        Os::Windows => "tally.exe",
        _ => "tally",
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
//
// These tests cross-validate against real packaging files in the monorepo
// (via include_str!) so they break if packaging structure drifts from
// what the extension expects.
// ---------------------------------------------------------------------------

#[cfg(test)]
mod tests {
    use super::*;

    /// The real tally-cli package.json from packaging/npm/.
    const TALLY_CLI_PACKAGE_JSON: &str = include_str!("../../../packaging/npm/package.json");

    /// The source-of-truth npm platform targets from packaging/npm/.
    const PLATFORM_TARGETS_JSON: &str =
        include_str!("../../../packaging/npm/platform-targets.json");

    /// The real pyproject.toml from packaging/pypi/.
    const PYPROJECT_TOML: &str = include_str!("../../../packaging/pypi/pyproject.toml");

    // -- has_tally_dependency: cross-validate against real package.json ------

    #[test]
    fn real_npm_package_is_named_tally_cli() {
        // The main npm package must be named "tally-cli" — our dependency
        // detection in has_tally_dependency hard-codes this name.
        let pkg: Value = serde_json::from_str(TALLY_CLI_PACKAGE_JSON).unwrap();
        assert_eq!(pkg["name"].as_str().unwrap(), "tally-cli");
    }

    #[test]
    fn has_tally_dependency_detects_real_optional_deps() {
        // A project that lists tally-cli as a dependency should be detected.
        // Build a minimal package.json that mirrors a real user project.
        let real_pkg: Value = serde_json::from_str(TALLY_CLI_PACKAGE_JSON).unwrap();
        let pkg_name = real_pkg["name"].as_str().unwrap();

        let user_project = format!(r#"{{"dependencies": {{"{pkg_name}": "^1.0.0"}}}}"#);
        assert!(has_tally_dependency(&user_project));

        let user_project_dev = format!(r#"{{"devDependencies": {{"{pkg_name}": "latest"}}}}"#);
        assert!(has_tally_dependency(&user_project_dev));
    }

    #[test]
    fn has_tally_dependency_negative_cases() {
        assert!(!has_tally_dependency(
            r#"{"dependencies": {"express": "^4.0.0"}}"#
        ));
        assert!(!has_tally_dependency("{}"));
        assert!(!has_tally_dependency("not json"));
    }

    // -- npm platform packages: cross-validate names and binary paths -------

    #[test]
    fn platform_targets_cover_expected_runtime_packages() {
        // The runtime package-name logic must stay aligned with the npm
        // platform target metadata used to generate published packages.
        let targets: Value = serde_json::from_str(PLATFORM_TARGETS_JSON).unwrap();

        let platforms: &[(&str, &str)] = &[
            ("darwin", "arm64"),
            ("darwin", "x64"),
            ("linux", "arm64"),
            ("linux", "x64"),
            ("windows", "arm64"),
            ("windows", "x64"),
            ("freebsd", "x64"),
        ];
        for (os, arch) in platforms {
            let name = format!("@wharflab/tally-{os}-{arch}");
            assert!(
                targets.as_array().unwrap().iter().any(|target| {
                    target["nodeOs"].as_str() == Some(*os)
                        && target["nodeArch"].as_str() == Some(*arch)
                }),
                "platform target metadata missing {name}"
            );
        }
    }

    #[test]
    fn real_npm_package_includes_runtime_platform_metadata() {
        let pkg: Value = serde_json::from_str(TALLY_CLI_PACKAGE_JSON).unwrap();
        let files: Vec<&str> = pkg["files"]
            .as_array()
            .unwrap()
            .iter()
            .map(|value| value.as_str().unwrap())
            .collect();
        assert!(
            files.contains(&"bin/"),
            "tally-cli package must include bin/: got {files:?}"
        );
        assert!(
            files.contains(&"platform-targets.json"),
            "tally-cli package must include platform-targets.json: got {files:?}"
        );
    }

    #[test]
    fn npm_binary_path_matches_platform_package_structure() {
        let real_name = "@wharflab/tally-darwin-arm64";

        let path = npm_binary_path(real_name, &Os::Mac);
        assert_eq!(path, format!("node_modules/{real_name}/bin/tally"));
    }

    #[test]
    fn npm_binary_path_windows_uses_exe_suffix() {
        let path = npm_binary_path("@wharflab/tally-windows-x64", &Os::Windows);
        assert!(
            path.ends_with("tally.exe"),
            "Windows binary path must end with tally.exe: got {path}"
        );
    }

    // -- pyproject.toml: cross-validate Python binary name ------------------

    #[test]
    fn real_pypi_package_exposes_tally_script() {
        // find_venv_binary looks for a binary named "tally" in the venv.
        // The pyproject.toml [project.scripts] section must declare this name.
        assert!(
            PYPROJECT_TOML.contains("tally = "),
            "pyproject.toml must declare a 'tally' script entry point"
        );
    }

    #[test]
    fn real_pypi_package_is_named_tally_cli() {
        // The pip package name is "tally-cli", matching what users `pip install`.
        assert!(
            PYPROJECT_TOML.contains(r#"name = "tally-cli""#),
            "pyproject.toml project name must be tally-cli"
        );
    }

    // -- github_release_asset: validate naming matches the release asset convention --

    #[test]
    fn github_asset_names_match_goreleaser_convention() {
        // Release assets: tally_{Version}_{MacOS|Linux|Windows}_{x86_64|arm64}.{tar.gz|zip}
        let cases: &[(Os, Architecture, &str, DownloadedFileType)] = &[
            (
                Os::Mac,
                Architecture::Aarch64,
                "tally_1.2.3_MacOS_arm64.tar.gz",
                DownloadedFileType::GzipTar,
            ),
            (
                Os::Mac,
                Architecture::X8664,
                "tally_1.2.3_MacOS_x86_64.tar.gz",
                DownloadedFileType::GzipTar,
            ),
            (
                Os::Linux,
                Architecture::Aarch64,
                "tally_1.2.3_Linux_arm64.tar.gz",
                DownloadedFileType::GzipTar,
            ),
            (
                Os::Linux,
                Architecture::X8664,
                "tally_1.2.3_Linux_x86_64.tar.gz",
                DownloadedFileType::GzipTar,
            ),
            (
                Os::Windows,
                Architecture::Aarch64,
                "tally_1.2.3_Windows_arm64.zip",
                DownloadedFileType::Zip,
            ),
            (
                Os::Windows,
                Architecture::X8664,
                "tally_1.2.3_Windows_x86_64.zip",
                DownloadedFileType::Zip,
            ),
        ];
        for (os, arch, expected_name, expected_type) in cases {
            let (name, file_type) = github_release_asset(os, arch, "1.2.3").unwrap();
            assert_eq!(
                &name, expected_name,
                "asset name mismatch for {os:?}/{arch:?}"
            );
            assert_eq!(
                &file_type, expected_type,
                "file type mismatch for {os:?}/{arch:?}"
            );
        }
    }

    #[test]
    fn github_asset_rejects_x86() {
        assert!(github_release_asset(&Os::Linux, &Architecture::X86, "1.0.0").is_err());
    }
}
