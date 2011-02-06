class DbSchemerCommand < Thor
  include Thor::Actions

  source_root(File.join(File.dirname(__FILE__)))

  attr_accessor :app_name

  desc "new PATH", "Create a new DbSchemer project"
  def new(path)
    self.app_name = File.basename path
    self.destination_root = path
    directory "new", "."
  end

  desc "migrate", "Drops definitions, run alterations, then recreate definitions"
  method_option :environment, :type => :string, :desc => "Database environment to load", :default => "development", :aliases => "-e"
  method_option :alteration_version, :type => :numeric, :desc => "Target alteration version", :aliases => "-a"
  method_option :definition_sequences, :type => :array, :desc => "Definition sequences to drop and create", :default => ["default"], :aliases => "-d"
  def migrate
    require 'yaml'

    config = YAML.load(File.read('config.yml'))
    db = Sequel.connect config['environments'][options["environment"]]
    db_schemer = DbSchemer.new(db, config['alterations']['table'], config['alterations']['column'], config['definitions']['table'])

    db_schemer.migrate(:version => options["alteration_version"], :sequences => options["definition_sequences"])
  end
end