# frozen_string_literal: true

require "minitest/autorun"
require "pathname"
require "tempfile"
require "yaml"

require_relative "generate_winget_manifests"

class GenerateWingetManifestsTest < Minitest::Test
  def test_normalized_version
    assert_equal "0.26.0", normalized_version("v0.26.0")
    assert_equal "0.26.0", normalized_version("0.26.0")
  end

  def test_parse_args_normalizes_release_date
    options = parse_args(%w[--version 0.26.0 --output-root /tmp/out --release-date 2026-03-15])

    assert_equal "2026-03-15", options[:release_date]
  end

  def test_read_checksums
    Dir.mktmpdir do |tmpdir|
      path = Pathname(tmpdir).join("tally_checksums.txt")
      path.write(<<~TEXT)
        ABCDEF tally_0.26.0_Windows_x86_64.exe
        123456 tally_0.26.0_Windows_arm64.exe
      TEXT

      assert_equal(
        {
          "tally_0.26.0_Windows_x86_64.exe" => "ABCDEF",
          "tally_0.26.0_Windows_arm64.exe" => "123456",
        },
        read_checksums(path),
      )
    end
  end

  def test_manifest_dir
    root = Pathname("/tmp/manifests")
    assert_equal(
      Pathname("/tmp/manifests/w/Wharflab/Tally/0.26.0"),
      manifest_dir(root, PACKAGE_IDENTIFIER, "0.26.0"),
    )
  end

  def test_dumped_manifests_include_expected_release_data
    version_manifest_data = version_manifest("0.26.0")
    assert_equal "Wharflab.Tally", version_manifest_data["PackageIdentifier"]
    assert_equal "en-US", version_manifest_data["DefaultLocale"]

    locale_manifest_data = default_locale_manifest("0.26.0", "wharflab", "tally")
    assert_equal "Wharflab", locale_manifest_data["Publisher"]
    assert_equal(
      "https://github.com/wharflab/tally/releases/tag/v0.26.0",
      locale_manifest_data["ReleaseNotesUrl"],
    )
    assert_equal(
      [{"DocumentLabel" => "Docs", "DocumentUrl" => "https://tally.wharflab.com/"}],
      locale_manifest_data["Documentations"],
    )

    installer_manifest_data = installer_manifest(
      "0.26.0",
      "wharflab",
      "tally",
      {
        "tally_0.26.0_Windows_x86_64.exe" => "ABCDEF",
        "tally_0.26.0_Windows_arm64.exe" => "123456",
      },
      "2026-03-15",
    )
    assert_equal "portable", installer_manifest_data["Installers"][0]["InstallerType"]
    assert_equal %w[tally docker-lint], installer_manifest_data["Commands"]
    assert_equal %w[tally docker-lint], installer_manifest_data["Installers"][0]["Commands"]
    assert_equal %w[dockerfile containerfile], installer_manifest_data["FileExtensions"]
    assert_equal "2026-03-15", installer_manifest_data["ReleaseDate"]
    assert_equal(
      "https://github.com/wharflab/tally/releases/download/v0.26.0/tally_0.26.0_Windows_x86_64.exe",
      installer_manifest_data["Installers"][0]["InstallerUrl"],
    )
  end

  def test_dump_manifest_writes_yaml_with_schema_comment
    Dir.mktmpdir do |tmpdir|
      path = Pathname(tmpdir).join("manifest.yaml")
      dump_manifest(path, "version", version_manifest("0.26.0"))

      content = path.read
      assert_includes content, "# Created by tally release automation"
      assert_includes content, "# yaml-language-server: $schema=https://aka.ms/winget-manifest.version.1.9.0.schema.json"

      parsed = YAML.safe_load(content.lines.drop(2).join)
      assert_equal "Wharflab.Tally", parsed["PackageIdentifier"]
      assert_equal "0.26.0", parsed["PackageVersion"]
    end
  end
end
