require 'sequel'
Sequel.extension :migration

require 'yaml'

config = YAML.load(File.read('config.yml'))
environment = ENV['environment'] || 'development'
version = Integer(ENV['version']) if ENV['version']
DB = Sequel.connect config['environments'][environment]
db_schemer = DbSchemer.new(DB, config['alterations']['table'], config['alterations']['column'], config['definitions']['table'])

desc "Run alterations then update definitions (options: environment=test, version=n)"
task :migrate do
  db_schemer.migrate(version)
end

desc "Run alterations (options: environment=test, version=n)"
task :alterations do
  db_schemer.run_alterations(version)
end

desc "Update definitions (options: environment=test)"
task :definitions do
  db_schemer.update_definitions
end
