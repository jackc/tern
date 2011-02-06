require 'sequel'
Sequel.extension :migration

require 'yaml'

config = YAML.load(File.read('config.yml'))
environment = ENV['environment'] || 'development'
version = Integer(ENV['version']) if ENV['version']
sequences = ENV['sequences'] ? ENV['sequences'].split(',') : ['default']
DB = Sequel.connect config['environments'][environment]
db_schemer = DbSchemer.new(DB, config['alterations']['table'], config['alterations']['column'], config['definitions']['table'])

desc "Drops definitions, run alterations, then recreate definitions (options: environment=test, version=n, sequences=expensive,default)"
task :migrate do
  db_schemer.migrate(:version => version, :sequences => sequences)
end
