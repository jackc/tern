class DbSchemer
  def initialize(db, alterations_table, alterations_column, definitions_table)
    @db = db
    @alterations_table = alterations_table.to_sym
    @alterations_column = alterations_column.to_sym
    @definitions_table = definitions_table.to_sym
  end

  def migrate(version=nil)
    @db.transaction do
      run_alterations
      update_definitions
    end
  end

  def run_alterations(version=nil)
    @db.transaction do
      Sequel::Migrator.run(@db, 'alterations', :table => @alterations_table, :column => @alterations_column, :target => version)
    end
  end

  def update_definitions
    @db.transaction do
      ensure_definitions_table_exists
      existing_definitions = load_existing_definitions
      target_definitions = load_target_definitions

      first_different_index = target_definitions.zip(existing_definitions).find_index do |target, exist|
        target.nil? || exist.nil? || exist[:create_sql] != target[:create_sql] || exist[:drop_sql] != target[:drop_sql]
      end

      return unless first_different_index

      existing_definitions_to_drop = existing_definitions[first_different_index..-1]
      target_definitions_to_create = target_definitions[first_different_index..-1]

      existing_definitions_to_drop.reverse.each do |definition|
        @db.run(definition[:drop_sql])
        @db[@definitions_table].filter(:id => definition[:id]).delete
      end

      target_definitions_to_create.each do |definition|
        @db.run(definition[:create_sql])
        @db[@definitions_table].insert definition
      end
    end
  end

  private
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

    def load_existing_definitions
      @db[@definitions_table].order(:id).all
    end

    def ensure_definitions_table_exists
      @db.create_table? @definitions_table do
        primary_key :id
        column :create_sql, :text
        column :drop_sql, :text
      end
    end
end
