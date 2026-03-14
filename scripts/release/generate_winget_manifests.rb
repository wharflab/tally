#!/usr/bin/env ruby
# frozen_string_literal: true

require "optparse"
require "pathname"
require "yaml"

PACKAGE_IDENTIFIER = "Wharflab.Tally"
PUBLISHER = "Wharflab"
PACKAGE_NAME = "Tally"
PACKAGE_LOCALE = "en-US"
MANIFEST_VERSION = "1.9.0"
SHORT_DESCRIPTION = "Dockerfile linter and formatter with first-class PowerShell and Windows container support."
TAG_LIST = %w[docker dockerfile containerfile linter].freeze
WINDOWS_ASSETS = [
  ["x64", "tally_%<version>s_Windows_x86_64.exe"],
  ["arm64", "tally_%<version>s_Windows_arm64.exe"],
].freeze

def parse_args(argv)
  options = {
    repo_owner: "wharflab",
    repo_name: "tally",
    dist_root: "dist",
  }

  parser = OptionParser.new do |opts|
    opts.banner = "Usage: generate_winget_manifests.rb --version VERSION --output-root PATH [options]"
    opts.on("--version VERSION") { |value| options[:version] = value }
    opts.on("--repo-owner OWNER") { |value| options[:repo_owner] = value }
    opts.on("--repo-name NAME") { |value| options[:repo_name] = value }
    opts.on("--dist-root PATH") { |value| options[:dist_root] = value }
    opts.on("--output-root PATH") { |value| options[:output_root] = value }
  end

  parser.parse!(argv)

  required = %i[version output_root]
  missing = required.select { |key| options[key].nil? || options[key].empty? }
  raise OptionParser::MissingArgument, missing.join(", ") unless missing.empty?

  options
end

def normalized_version(value)
  value.sub(/\Av/, "")
end

def read_checksums(path)
  path.each_line.with_object({}) do |line, checksums|
    parts = line.strip.split
    raise "invalid checksum line: #{line.inspect}" unless parts.length == 2

    checksums[parts[1]] = parts[0].upcase
  end
end

def manifest_dir(output_root, package_identifier, version)
  parts = package_identifier.split(".")
  Pathname(output_root).join(parts[0][0].downcase, *parts, version)
end

def github_release_url(owner, repo, version, filename)
  "https://github.com/#{owner}/#{repo}/releases/download/v#{version}/#{filename}"
end

def schema_comment(kind)
  "# yaml-language-server: $schema=https://aka.ms/winget-manifest.#{kind}.#{MANIFEST_VERSION}.schema.json"
end

def dump_manifest(path, kind, data)
  path.dirname.mkpath
  yaml = YAML.dump(data)
  content = +"# Created by tally release automation\n"
  content << "#{schema_comment(kind)}\n\n"
  content << yaml.sub(/\A---\s*\n/, "")
  path.write(content)
end

def version_manifest(version)
  {
    "PackageIdentifier" => PACKAGE_IDENTIFIER,
    "PackageVersion" => version,
    "DefaultLocale" => PACKAGE_LOCALE,
    "ManifestType" => "version",
    "ManifestVersion" => MANIFEST_VERSION,
  }
end

def default_locale_manifest(version, owner, repo)
  {
    "PackageIdentifier" => PACKAGE_IDENTIFIER,
    "PackageVersion" => version,
    "PackageLocale" => PACKAGE_LOCALE,
    "Publisher" => PUBLISHER,
    "PublisherUrl" => "https://github.com/#{owner}",
    "PublisherSupportUrl" => "https://github.com/#{owner}/#{repo}/issues",
    "PackageName" => PACKAGE_NAME,
    "PackageUrl" => "https://github.com/#{owner}/#{repo}",
    "ShortDescription" => SHORT_DESCRIPTION,
    "Moniker" => "tally",
    "License" => "GPL-3.0-only",
    "LicenseUrl" => "https://github.com/#{owner}/#{repo}/blob/main/LICENSE",
    "ReleaseNotesUrl" => "https://github.com/#{owner}/#{repo}/releases/tag/v#{version}",
    "Tags" => TAG_LIST,
    "ManifestType" => "defaultLocale",
    "ManifestVersion" => MANIFEST_VERSION,
  }
end

def installer_manifest(version, owner, repo, checksums)
  installers = WINDOWS_ASSETS.map do |architecture, pattern|
    filename = format(pattern, version: version)
    sha256 = checksums.fetch(filename) { raise "missing checksum for #{filename}" }
    {
      "Architecture" => architecture,
      "InstallerType" => "portable",
      "InstallerUrl" => github_release_url(owner, repo, version, filename),
      "InstallerSha256" => sha256,
      "Commands" => ["tally"],
    }
  end

  {
    "PackageIdentifier" => PACKAGE_IDENTIFIER,
    "PackageVersion" => version,
    "Commands" => ["tally"],
    "Installers" => installers,
    "ManifestType" => "installer",
    "ManifestVersion" => MANIFEST_VERSION,
  }
end

def main(argv = ARGV)
  options = parse_args(argv)
  version = normalized_version(options[:version])
  dist_root = Pathname(options[:dist_root]).expand_path.realpath
  checksums = read_checksums(dist_root.join("tally_checksums.txt"))
  out_dir = manifest_dir(Pathname(options[:output_root]).expand_path, PACKAGE_IDENTIFIER, version)

  dump_manifest(
    out_dir.join("#{PACKAGE_IDENTIFIER}.yaml"),
    "version",
    version_manifest(version),
  )
  dump_manifest(
    out_dir.join("#{PACKAGE_IDENTIFIER}.locale.#{PACKAGE_LOCALE}.yaml"),
    "defaultLocale",
    default_locale_manifest(version, options[:repo_owner], options[:repo_name]),
  )
  dump_manifest(
    out_dir.join("#{PACKAGE_IDENTIFIER}.installer.yaml"),
    "installer",
    installer_manifest(version, options[:repo_owner], options[:repo_name], checksums),
  )

  puts out_dir
  0
end

if $PROGRAM_NAME == __FILE__
  exit(main)
end
