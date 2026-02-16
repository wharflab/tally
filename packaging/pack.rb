#!/usr/bin/env ruby

require "fileutils"
require "json"

# Version from VERSION env var (strips leading 'v' if present), or fallback
VERSION = (ENV["VERSION"] || "0.1.0").sub(/^v/, "")

ROOT = File.join(__dir__, "..")
DIST = File.join(ROOT, "dist")

# Platforms with binaries and PEP425 wheel tags (no freebsd - no standard wheel tag)
PYTHON_PLATFORMS = [
  ["linux", "x86_64"], ["linux", "arm64"],
  ["darwin", "x86_64"], ["darwin", "arm64"],
  ["windows", "x86_64"], ["windows", "arm64"]
].freeze

module Pack
  extend FileUtils

  module_function

  def prepare
    puts "Preparing release for version #{VERSION}"
    clean
    set_version
    put_additional_files
    put_binaries
  end

  def clean
    cd(__dir__)
    puts "Cleaning... "
    rm(Dir["npm/**/README.md"])
    rm(Dir["npm/**/tally*"].filter(&File.method(:file?)))
    system("git clean -fdX npm/ pypi/ rubygems/", exception: true)
    puts "done"
  end

  def set_version
    cd(__dir__)
    puts "Replacing version to #{VERSION} in packages"

    # Update NPM packages
    Dir["npm/**/package.json"].each do |package_json|
      replace_in_file(package_json, /"version": "[\d.]+"/, %{"version": "#{VERSION}"})
    end

    # Update main NPM package optional dependencies
    replace_in_file("npm/tally/package.json",
                   /"(@wharflab\/tally-.+)": "[\d.]+"/,
                   %{"\\1": "#{VERSION}"})

    # Update PyPI version
    replace_in_file("pypi/pyproject.toml", /(version\s*=\s*)"[^"]+"/, %{\\1"#{VERSION}"})

    # Update Rubygems version
    replace_in_file("rubygems/tally-cli.gemspec", /(spec\.version\s+=\s+)"[^"]+"/, %{\\1"#{VERSION}"})
  end

  def put_additional_files
    cd(__dir__)
    puts "Putting README, LICENSE, and NOTICE... "
    Dir["npm/*"].each do |npm_dir|
      cp(File.join(ROOT, "README.md"), File.join(npm_dir, "README.md"), verbose: true)
      cp(File.join(ROOT, "LICENSE"), File.join(npm_dir, "LICENSE"), verbose: true)
      cp(File.join(ROOT, "NOTICE"), File.join(npm_dir, "NOTICE"), verbose: true)
    end
    puts "done"
  end

  # Map goreleaser output directories to package destinations
  # Goreleaser outputs: tally_{goos}_{goarch}_{variant}/tally
  # where variant is v1 for amd64, v8.0 for arm64
  BINARY_MAPPING = {
    # [goos, goarch, variant, extension]
    ["linux",   "amd64", "v1",   ""]     => { npm: "linux-x64",    gem: "linux-x64",    pypi: "linux-x86_64" },
    ["linux",   "arm64", "v8.0", ""]     => { npm: "linux-arm64",  gem: "linux-arm64",  pypi: "linux-arm64" },
    ["darwin",  "amd64", "v1",   ""]     => { npm: "darwin-x64",   gem: "darwin-x64",   pypi: "darwin-x86_64" },
    ["darwin",  "arm64", "v8.0", ""]     => { npm: "darwin-arm64", gem: "darwin-arm64", pypi: "darwin-arm64" },
    ["windows", "amd64", "v1",   ".exe"] => { npm: "windows-x64",  gem: "windows-x64",  pypi: "windows-x86_64" },
    ["windows", "arm64", "v8.0", ".exe"] => { npm: "windows-arm64",gem: "windows-arm64",pypi: "windows-arm64" },
    ["freebsd", "amd64", "v1",   ""]     => { npm: "freebsd-x64",  gem: "freebsd-x64",  pypi: "freebsd-x86_64" },
  }.freeze

  def put_binaries
    cd(__dir__)
    puts "Putting binaries to packages..."

    BINARY_MAPPING.each do |(goos, goarch, variant, ext), targets|
      source_dir = "#{DIST}/tally_#{goos}_#{goarch}_#{variant}"
      source = "#{source_dir}/tally#{ext}"

      unless File.exist?(source)
        puts "Skipping #{source} (not found)"
        next
      end

      # NPM
      npm_dest = "npm/tally-#{targets[:npm]}/bin/tally#{ext}"
      mkdir_p(File.dirname(npm_dest))
      cp(source, npm_dest, verbose: true)

      # Rubygems
      gem_dest = "rubygems/libexec/tally-#{targets[:gem]}/tally#{ext}"
      mkdir_p(File.dirname(gem_dest))
      cp(source, gem_dest, verbose: true)

      # PyPI
      pypi_dest = "pypi/tally_cli/bin/tally-#{targets[:pypi]}/tally#{ext}"
      mkdir_p(File.dirname(pypi_dest))
      cp(source, pypi_dest, verbose: true)
    end

    puts "done"
  end

  def publish
    publish_pypi
    publish_npm
    publish_gem
  end

  def publish_npm
    puts "Publishing tally npm..."
    cd(File.join(__dir__, "npm"))
    Dir["tally*"].each do |package|
      puts "publishing #{package}"
      cd(File.join(__dir__, "npm", package))
      system("npm publish --access public", exception: false)
      cd(File.join(__dir__, "npm"))
    end
  end

  def publish_gem
    puts "Publishing to Rubygems..."
    cd(File.join(__dir__, "rubygems"))
    system("rake build", exception: true)
    system("gem push pkg/*.gem", exception: true)
  end

  def publish_pypi
    puts "Publishing to PyPI..."
    pypi_dir = File.join(__dir__, "pypi")

    PYTHON_PLATFORMS.each do |os, arch|
      puts "Building wheel for #{os}-#{arch}..."
      cd(pypi_dir)
      ENV["TALLY_TARGET_PLATFORM"] = os
      ENV["TALLY_TARGET_ARCH"] = arch
      system("uv build --wheel", exception: true)
    end

    puts "Uploading to PyPI..."
    system("uv publish", exception: true)
  end

  def replace_in_file(filepath, regexp, value)
    text = File.open(filepath, "r") do |f|
      f.read
    end
    text.gsub!(regexp, value)
    File.open(filepath, "w") do |f|
      f.write(text)
    end
  end
end

ARGV.each do |cmd|
  Pack.public_send(cmd)
end
