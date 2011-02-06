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
end