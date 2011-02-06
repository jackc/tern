class DbSchemer
  def initialize(db, alterations_table, alterations_column, definitions_table)
    @db = db
    @alterations_table = alterations_table.to_sym
    @alterations_column = alterations_column.to_sym
    @definitions_table = definitions_table.to_sym
    ensure_definitions_table_exists
  end

  def migrate(version=nil)
    @db.transaction do
      drop_existing_definitions
      run_alterations
      create_target_definitions
    end
  end

  private
    def run_alterations(version=nil)
      Sequel::Migrator.run(@db, 'alterations', :table => @alterations_table, :column => @alterations_column, :target => version)
    end

    def drop_existing_definitions
      existing_definitions = load_existing_definitions

      existing_definitions.reverse.each do |definition|
        @db.run(definition[:drop_sql])
        @db[@definitions_table].filter(:id => definition[:id]).delete
      end
    end

    def load_existing_definitions
      @db[@definitions_table].order(:id).all
    end

    def create_target_definitions
      target_definitions = load_target_definitions

      target_definitions.each do |definition|
        @db.run(definition[:create_sql])
        @db[@definitions_table].insert definition
      end
    end

    def load_target_definitions
      File.readlines('definitions/sequence').map do |l|
        l.sub(/#.*$/, '')
      end.map do |l|
        l.strip
      end.reject do |l|
        l.empty?
      end.map do |l|
        create_sql, drop_sql = File.read('definitions/' + l).split('---- CREATE above / DROP below ----')
        {:create_sql => create_sql, :drop_sql => drop_sql}
      end
    end

    def ensure_definitions_table_exists
      @db.create_table? @definitions_table do
        primary_key :id
        column :create_sql, :text
        column :drop_sql, :text
      end
    end
end
