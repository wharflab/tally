#!/usr/bin/env ruby

require "fileutils"
require "json"

# Version from VERSION env var (strips leading 'v' if present), or fallback
VERSION = (ENV["VERSION"] || "0.1.0").sub(/^v/, "")
GEM_VERSION = (ENV["GEM_VERSION"] || VERSION).sub(/^v/, "")

ROOT = File.join(__dir__, "..")
DIST = File.join(ROOT, "dist")

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
    system("git clean -fdX rubygems/", exception: true)
    puts "done"
  end

  def set_version
    cd(__dir__)
    puts "Replacing versions in packages"

    # Update Rubygems version
    replace_in_file("rubygems/tally-cli.gemspec", /(spec\.version\s+=\s+)"[^"]+"/, %{\\1"#{GEM_VERSION}"})
  end

  def put_additional_files
    cd(__dir__)
    puts "Putting README, LICENSE, and NOTICE... "
    cp(File.join(ROOT, "README.md"), File.join("rubygems", "README.md"), verbose: true)
    cp(File.join(ROOT, "LICENSE"), File.join("rubygems", "LICENSE"), verbose: true)
    cp(File.join(ROOT, "NOTICE"), File.join("rubygems", "NOTICE"), verbose: true)
    puts "done"
  end

  # Map release dist directories to RubyGems destinations.
  # Release builds populate: tally_{goos}_{goarch}_{variant}/tally
  # where variant is v1 for amd64, v8.0 for arm64
  BINARY_MAPPING = {
    ["linux",   "amd64", "v1",   ""]     => "linux-x64",
    ["linux",   "arm64", "v8.0", ""]     => "linux-arm64",
    ["darwin",  "amd64", "v1",   ""]     => "darwin-x64",
    ["darwin",  "arm64", "v8.0", ""]     => "darwin-arm64",
    ["windows", "amd64", "v1",   ".exe"] => "windows-x64",
    ["windows", "arm64", "v8.0", ".exe"] => "windows-arm64",
    ["freebsd", "amd64", "v1",   ""]     => "freebsd-x64",
  }.freeze

  def put_binaries
    cd(__dir__)
    puts "Putting binaries to packages..."

    BINARY_MAPPING.each do |(goos, goarch, variant, ext), target|
      source_dir = "#{DIST}/tally_#{goos}_#{goarch}_#{variant}"
      source = "#{source_dir}/tally#{ext}"

      unless File.exist?(source)
        puts "Skipping #{source} (not found)"
        next
      end

      # Rubygems
      gem_dest = "rubygems/libexec/tally-#{target}/tally#{ext}"
      mkdir_p(File.dirname(gem_dest))
      cp(source, gem_dest, verbose: true)

    end

    puts "done"
  end

  def publish
    publish_gem
  end

  def publish_gem
    puts "Publishing to Rubygems..."
    cd(File.join(__dir__, "rubygems"))
    system("rake build", exception: true)
    if ENV["GEM_PUBLISH_DRY_RUN"] == "1"
      puts "Skipping gem push (dry run)"
      return
    end
    system("gem push pkg/*.gem", exception: true)
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
