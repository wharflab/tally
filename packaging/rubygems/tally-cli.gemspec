Gem::Specification.new do |spec|
  spec.name          = "tally-cli"
  spec.version       = "0.1.0"
  spec.authors       = ["Konstantin Vyatkin"]
  spec.email         = ["tino@vtkn.io"]

  spec.summary       = "A fast, configurable linter for Dockerfiles and Containerfiles"
  spec.homepage      = "https://github.com/wharflab/tally"
  spec.post_install_message = "tally installed! Run 'tally --help' to see usage."

  spec.bindir        = "bin"
  spec.executables   << "tally"
  spec.require_paths = ["lib"]

  spec.files = %w(
    lib/tally-cli.rb
    bin/tally
    LICENSE
    NOTICE
  ) + `find libexec/ -executable -type f -print0`.split("\x0")

  spec.licenses = ['Apache-2.0']
end
