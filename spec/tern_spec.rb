require 'rspec'
require 'fileutils'
require 'sequel'
require 'tern'

$tmpdir = 'spec/tmp/' + Time.now.strftime("%Y-%m-%d-%H-%M-%S")
FileUtils.mkdir_p $tmpdir

RSpec.configure do |config|
  def tern(args="")
    lib = File.expand_path(File.join(File.dirname(__FILE__), '..', 'lib'))
    bin = File.expand_path(File.join(File.dirname(__FILE__), '..', 'bin', 'tern'))
    `ruby -I #{lib} #{bin} #{args}`
  end

  def tern_migrate(path, args="")
    Dir.chdir(path) do
      tern "migrate #{args}"
    end
  end

  def tern_generate(path, args="")
    Dir.chdir(path) do
      tern "generate #{args}"
    end
  end

  def tmpdir
    $tmpdir
  end
end

describe "Change" do
  describe "parse" do
    it "splits on SPLIT_MARKER" do
      ::Change.parse("create #{::Change::SPLIT_MARKER} drop").should == ["create ", " drop"]
    end

    it "returns entire string as create if no SPLIT_MARKER not found" do
      ::Change.parse("create").should == ["create", nil]
    end
  end
end

describe "tern" do
  it "displays task list when called without arguments" do
    tern.should match(/Tasks/)
  end

  describe "new" do
    it "creates a new project at path" do
      Dir.chdir(tmpdir) do
        tern "new tern_spec"
        File.exist?("tern_spec").should be_true
        File.exist?("tern_spec/alterations").should be_true
        File.exist?("tern_spec/definitions").should be_true
        File.exist?("tern_spec/config.yml").should be_true
      end
    end
  end

  describe "generate" do
    before(:each) do |example|
      @project_path = File.expand_path(File.join(tmpdir, example.object_id.to_s))
      tern "new #{@project_path}"
    end

    it "prints an error message if config.yml is not found" do
      tern_generate("spec/tmp", "alteration create_people").should match(/This directory does not appear to be a Tern project. config.yml not found./)
    end

    describe "alteration" do
      it "creates alteration file with next available number prefixing name" do
        tern_generate(@project_path, "alteration create_widgets")
        File.exist?(File.join(@project_path, "alterations", "001_create_widgets.rb")).should be_true
        tern_generate(@project_path, "alteration create_sprockets")
        File.exist?(File.join(@project_path, "alterations", "002_create_sprockets.rb")).should be_true
      end
    end
    
    describe "definition" do
      it "creates definition file" do
        tern_generate(@project_path, "definition create_a_view")
        File.exist?(File.join(@project_path, "definitions", "create_a_view.sql")).should be_true
      end
    end
  end

  describe "migrate" do
    before(:each) do
      `createdb tern_test_development`
      @dev_db = Sequel.connect :adapter => :postgres, :database => 'tern_test_development'
    end

    after(:each) do
      @dev_db.disconnect
      `dropdb tern_test_development`
    end

    it "prints an error message if config.yml is not found" do
      tern_migrate("spec/tmp").should match(/This directory does not appear to be a Tern project. config.yml not found./)
    end

    it "works without any alterations" do
      tern_migrate("spec/projects/no_alterations").should be_empty
    end

    it "creates definitions table on first run" do
      tern_migrate "spec/projects/new"
      @dev_db.tables.should include(:tern_definitions)
    end

    it "creates alterations table on first run" do
      tern_migrate "spec/projects/new"
      @dev_db.tables.should include(:tern_alterations)
    end

    it "applies alterations" do
      tern_migrate "spec/projects/alterations"
      @dev_db.tables.should include(:people)
    end

    it "reverts alterations to given alteration version" do
      tern_migrate "spec/projects/alterations"
      tern_migrate "spec/projects/alterations", "-a 0"
      @dev_db.tables.should_not include(:people)
    end

    it "uses environment parameter" do
      `createdb tern_test_test`
      test_db = Sequel.connect :adapter => :postgres, :database => 'tern_test_test'

      tern_migrate "spec/projects/new", "-e test"
      test_db.tables.should include(:tern_definitions)
      @dev_db.tables.should_not include(:tern_definitions)
      
      test_db.disconnect
      `dropdb tern_test_test`
    end

    it "creates definitions" do
      tern_migrate "spec/projects/definitions"
      @dev_db.get{ultimate_answer{}}.should == 42
    end

    it "drops definitions" do
      tern_migrate "spec/projects/definitions"
      tern_migrate "spec/projects/new"
      expect { @dev_db.get{ultimate_answer{}} }.to raise_error(Sequel::DatabaseError)
    end

    it "creates definitions after alterations" do
      tern_migrate "spec/projects/dependencies_1"
      @dev_db.tables.should include(:widgets)
      expect { @dev_db[:widgets_view].all }.to_not raise_error(Sequel::DatabaseError)
    end

    it "drops existing definitions" do
      tern_migrate "spec/projects/dependencies_1"
      tern_migrate "spec/projects/dependencies_2"
      expect { @dev_db[:widgets_view].all }.to raise_error(Sequel::DatabaseError)
    end

    context "multiple definition sequences" do
      it "creates specified definition sequences in order" do
        tern_migrate "spec/projects/multiple_sequences", "-d expensive default"
        expect { @dev_db[:a].all }.to_not raise_error(Sequel::DatabaseError)
        expect { @dev_db[:b].all }.to_not raise_error(Sequel::DatabaseError)
      end

      it "creates specified definition sequence" do
        tern_migrate "spec/projects/multiple_sequences", "-d expensive"
        expect { @dev_db[:a].all }.to_not raise_error(Sequel::DatabaseError)
      end

      it "ignores unspecified definition sequences" do
        tern_migrate "spec/projects/multiple_sequences", "-d expensive"
        expect { @dev_db[:b].all }.to raise_error(Sequel::DatabaseError)
      end

      it "removes specified definition sequence from database that does not exist in definitions" do
        tern_migrate "spec/projects/multiple_sequences", "-d expensive"
        tern_migrate "spec/projects/new", "-d expensive"
        expect { @dev_db[:a].all }.to raise_error(Sequel::DatabaseError)
      end
    end
  end
end