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

  map "m" => "migrate"
  desc "migrate", "Drop definitions, run alterations, then recreate definitions"
  method_option :environment, :type => :string, :desc => "Database environment to load", :default => "development", :aliases => "-e"
  method_option :alteration_version, :type => :numeric, :desc => "Target alteration version", :aliases => "-a"
  method_option :definition_sequences, :type => :array, :desc => "Definition sequences to drop and create", :default => ["default"], :aliases => "-d"
  def migrate
    require 'yaml'

    unless File.exist?('config.yml')
      say 'This directory does not appear to be a Tern project. config.yml not found.'
      return
    end
    config = YAML.load(File.read('config.yml'))
    ::Kernel.const_set("DB", Sequel.connect(config['environments'][options["environment"]])) # using const_set to avoid dynamic constant assignment error

    begin
      tern = Tern.new(config['alterations']['table'], config['alterations']['column'], config['definitions']['table'])
      tern.migrate(:version => options["alteration_version"], :sequences => options["definition_sequences"])
    rescue TernError
      say $!, :red
    end
  end

  map "g" => "generate"
  desc "generate TYPE NAME", "Generate files"
  def generate(type, name)
    unless File.exist?('config.yml')
      say 'This directory does not appear to be a Tern project. config.yml not found.', :red
      return
    end

    case type
    when 'a', 'alteration'
      current_version = Dir.entries('alterations').map do |f|
        f =~ /^(\d+)_.*.sql$/ ? $1.to_i : 0
      end.max
      zero_padded_next_version = (current_version+1).to_s.rjust(3, "0")
      file_name = "#{zero_padded_next_version}_#{name}.sql"
      copy_file "change.sql", "alterations/#{file_name}"
    when 'd', 'definition'
      file_name = "#{name}.sql"
      copy_file "change.sql", "definitions/#{file_name}"
      say "Remember to add #{file_name} in your sequence.yml file."
    else
      say "#{type} is not a valid TYPE", :red
    end
  end
end