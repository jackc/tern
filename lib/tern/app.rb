require 'thor'
require 'thor/group'

class App < Thor
  include Thor::Actions

  source_root(File.join(File.dirname(__FILE__), "generators"))

  attr_accessor :app_name

  desc "new PATH", "Create a new Tern project"
  def new(path)
    self.app_name = File.basename path
    self.destination_root = path
    directory "new", "."
  end

  desc "migrate", "Drop definitions, run alterations, then recreate definitions"
  method_option :environment, :type => :string, :desc => "Database environment to load", :default => "development", :aliases => "-e"
  method_option :alteration_version, :type => :numeric, :desc => "Target alteration version", :aliases => "-a"
  method_option :definition_sequences, :type => :array, :desc => "Definition sequences to drop and create", :default => ["default"], :aliases => "-d"
  def migrate
    require 'yaml'

    config = YAML.load(File.read('config.yml'))
    db = Sequel.connect config['environments'][options["environment"]]
    tern = Tern.new(db, config['alterations']['table'], config['alterations']['column'], config['definitions']['table'])

    tern.migrate(:version => options["alteration_version"], :sequences => options["definition_sequences"])
  end
end