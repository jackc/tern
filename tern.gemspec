# -*- encoding: utf-8 -*-
$:.push File.expand_path("../lib", __FILE__)

Gem::Specification.new do |s|
  s.name        = "tern"
  s.version     = '0.7.1'
  s.platform    = Gem::Platform::RUBY
  s.authors     = ["Jack Christensen"]
  s.email       = ["jack@jackchristensen.com"]
  s.homepage    = "https://github.com/JackC/tern"
  s.summary     = %q{The SQL Fan's Migrator}
  s.description = %q{Manages schemas with views, functions, triggers along with traditional migrations}

  s.add_dependency "sequel", ">= 3.19.0"
  s.add_dependency "thor", ">= 0.14.6"

  s.add_development_dependency 'rspec', '>= 2.4.0'

  s.files         = `git ls-files`.split("\n")
  s.test_files    = `git ls-files -- {test,spec,features}/*`.split("\n")
  s.executables   = `git ls-files -- bin/*`.split("\n").map{ |f| File.basename(f) }
  s.require_paths = ["lib"]
end
