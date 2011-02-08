require 'sequel'
require 'yaml'

class Change
  SPLIT_MARKER = '---- CREATE above / DROP below ----'

  def self.parse(string)
    create_sql, drop_sql = string.split('---- CREATE above / DROP below ----')
    [create_sql, drop_sql]
  end
end

class Definition < Change
  class << self
    attr_accessor :table_name

    def table
      DB[table_name]
    end

    def ensure_table_exists
      DB.create_table? table_name do
        primary_key :id
        column :sequence, :text, :null => false
        column :create_sql, :text, :null => false
        column :drop_sql, :text, :null => false
      end
    end

    def load_existing
      table.order(:id).all.map do |row|
        new row[:id], row[:sequence], row[:create_sql], row[:drop_sql]
      end.group_by { |d| d.sequence }
    end

    def load_targets(sequence_yml_path)
      definition_sequences = YAML.load(File.read(sequence_yml_path))
      sequence_dir = File.dirname(sequence_yml_path)

      definition_sequences.keys.each do |sequence|
        definition_sequences[sequence] = definition_sequences[sequence].map do |f|
          create_sql, drop_sql = parse File.read(File.join(sequence_dir, f))
          Definition.new nil, sequence, create_sql, drop_sql
        end
      end

      definition_sequences
    end
  end

  attr_reader :id
  attr_reader :sequence
  attr_reader :create_sql
  attr_reader :drop_sql

  def initialize(id, sequence, create_sql, drop_sql)
    @id = id
    @sequence = sequence
    @create_sql = create_sql
    @drop_sql = drop_sql
  end

  def create
    DB.run create_sql
    table.insert :sequence => sequence, :create_sql => create_sql, :drop_sql => drop_sql
  end

  def drop
    DB.run drop_sql
    table.filter(:id => id).delete
  end

  def table
    self.class.table
  end
end

class Alteration < Change
  class IrreversibleAlteration < StandardError
  end

  class MissingAlteration < StandardError
  end

  class DuplicateAlteration < StandardError
  end

  class << self
    attr_accessor :table_name
    attr_accessor :version_column

    def table
      DB[table_name]
    end

    def ensure_table_exists
      vc = version_column # because create_table? block is run with different binding and can't access version_column
      DB.create_table? table_name do
        column vc, :integer, :null => false
      end
      table.insert version_column => 0
    end

    def version
      table.get(version_column)
    end

    def version=(new_version)
      table.update version_column => new_version
    end

    def load(alterations_path)
      alterations = Dir.glob("#{alterations_path}/[0-9]*.sql").map do |path|
        raise "This can't happen" unless File.basename(path) =~ /^(\d+)/
        version = $1.to_i
        create_sql, drop_sql = parse File.read(path)
        new version, create_sql, drop_sql
      end.sort_by(&:version)

      alterations.each_with_index do |a, i|
        expected = i+1
        raise DuplicateAlteration, "Alteration #{a.version.to_s.rjust(3, "0")} is duplicated" if a.version < expected
        raise MissingAlteration, "Alteration #{expected.to_s.rjust(3, "0")} is missing" if a.version > expected
      end

      alterations
    end
  end

  attr_reader :version
  attr_reader :create_sql
  attr_reader :drop_sql

  def initialize(version, create_sql, drop_sql)
    @version = version
    @create_sql = create_sql
    @drop_sql = drop_sql
  end

  def create
    DB.run create_sql
    Alteration.version = version
  end

  def drop
    raise IrreversibleAlteration, "Alteration #{version.to_s.rjust(3, "0")} is irreversible" unless drop_sql
    DB.run drop_sql
    Alteration.version = version - 1
  end
end

class Tern
  def initialize(alterations_table, alterations_column, definitions_table)
    Alteration.table_name = alterations_table.to_sym
    Alteration.version_column = alterations_column.to_sym
    Alteration.ensure_table_exists
    @alterations = Alteration.load 'alterations'

    Definition.table_name = definitions_table.to_sym
    Definition.ensure_table_exists
    @existing_definitions = Definition.load_existing
    @target_definitions = Definition.load_targets 'definitions/sequence.yml'
  end

  def migrate(options={})
    sequences = options[:sequences] || ['default']
    DB.transaction do
      drop_existing_definitions(sequences)
      run_alterations(options[:version])
      create_target_definitions(sequences)
    end
  end

  private
    def run_alterations(version=nil)
      return if @alterations.empty?
      version ||= @alterations.size

      if Alteration.version < version
        @alterations[Alteration.version..version].each(&:create)
      elsif
        @alterations[version..(Alteration.version-1)].reverse.each(&:drop)
      end
    end

    def drop_existing_definitions(sequences)
      sequences.each do |s|
        sequence = @existing_definitions[s]
        if sequence
          sequence.reverse.each do |definition|
            definition.drop
          end
        end
      end
    end

    def create_target_definitions(sequences)
      sequences.each do |s|
        sequence = @target_definitions[s]
        if sequence
          sequence.each do |definition|
            definition.create
          end
        end
      end
    end
end
