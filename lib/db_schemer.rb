require 'yaml'

class DbSchemer
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
      Sequel::Migrator.run(@db, 'alterations', :table => @alterations_table, :column => @alterations_column, :target => version)
    end

    def drop_existing_definitions(sequences)
      sequences.each do |s|
        @existing_definitions[s].reverse.each do |definition|
        @db.run(definition[:drop_sql])
        @db[@definitions_table].filter(:id => definition[:id]).delete
      end
    end

    def load_existing_definitions
      @db[@definitions_table].order(:id).all.group_by { |d| d[:sequence] }
    end

    def create_target_definitions(sequences)
      sequences.each do |s|
        @target_definitions[s].each do |definition|
          @db.run(definition[:create_sql])
          @db[@definitions_table].insert definition
        end
      end
    end

    def load_target_definitions
      YAML.load(File.read('definitions/sequence.yml')).map do |sequence, file_names|
        file_names.each do |f|
          create_sql, drop_sql = File.read('definitions/' + f).split('---- CREATE above / DROP below ----')
          {:create_sql => create_sql, :drop_sql => drop_sql}
        end
      end
    end

    def ensure_definitions_table_exists
      @db.create_table? @definitions_table do
        primary_key :id
        column :sequence, :text
        column :create_sql, :text
        column :drop_sql, :text
      end
    end
end
