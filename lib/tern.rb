require 'sequel'
Sequel.extension :migration
require 'yaml'

class Tern
  def initialize(db, alterations_table, alterations_column, definitions_table)
    @db = db
    @alterations_table = alterations_table.to_sym
    @alterations_column = alterations_column.to_sym
    @definitions_table = definitions_table.to_sym

    ensure_definitions_table_exists
    @existing_definitions = load_existing_definitions
    @target_definitions = load_target_definitions
  end

  def migrate(options={})
    sequences = options[:sequences] || ['default']
    @db.transaction do
      drop_existing_definitions(sequences)
      run_alterations(options[:version])
      create_target_definitions(sequences)
    end
  end

  private
    def run_alterations(version=nil)
      unless Dir.glob("alterations/[0-9]*.rb").empty?
        Sequel::Migrator.run(@db, 'alterations', :table => @alterations_table, :column => @alterations_column, :target => version)
      end
    end

    def drop_existing_definitions(sequences)
      sequences.each do |s|
        sequence = @existing_definitions[s]
        if sequence
          sequence.reverse.each do |definition|
            @db.run(definition[:drop_sql])
            @db[@definitions_table].filter(:id => definition[:id]).delete
          end
        end
      end
    end

    def load_existing_definitions
      @db[@definitions_table].order(:id).all.group_by { |d| d[:sequence] }
    end

    def create_target_definitions(sequences)
      sequences.each do |s|
        sequence = @target_definitions[s]
        if sequence
          sequence.each do |definition|
            @db.run(definition[:create_sql])
            @db[@definitions_table].insert :sequence => s, :create_sql => definition[:create_sql], :drop_sql => definition[:drop_sql]
          end
        end
      end
    end

    def load_target_definitions
      definition_sequences = YAML.load(File.read('definitions/sequence.yml'))
      
      definition_sequences.keys.each do |sequence|
        definition_sequences[sequence] = definition_sequences[sequence].map do |f|
          create_sql, drop_sql = File.read('definitions/' + f).split('---- CREATE above / DROP below ----')
          {:create_sql => create_sql, :drop_sql => drop_sql}
        end
      end

      definition_sequences
    end

    def ensure_definitions_table_exists
      @db.create_table? @definitions_table do
        primary_key :id
        column :sequence, :text, :null => false
        column :create_sql, :text, :null => false
        column :drop_sql, :text, :null => false
      end
    end
end
