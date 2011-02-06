require 'sequel'
Sequel.extension :migration

require 'yaml'

config = YAML.load(File.read('config.yml'))
environment = ENV['environment'] || 'development'
version = Integer(ENV['version']) if ENV['version']
DB = Sequel.connect config['environments'][environment]
db_schemer = DbSchemer.new(DB, config['alterations']['table'], config['alterations']['column'], config['definitions']['table'])

desc "Drops definitions, run alterations, then recreate definitions (options: environment=test, version=n)"
task :migrate do
  db_schemer.migrate(version)
end
