require 'rspec'
require 'fileutils'
require 'sequel'

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

  def tmpdir
    $tmpdir
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

  describe "migrate" do
    before(:each) do
      `createdb tern_test_development`
      @dev_db = Sequel.connect :adapter => :postgres, :database => 'tern_test_development'
    end

    after(:each) do
      @dev_db.disconnect
      `dropdb tern_test_development`
    end

    it "works without any alterations" do
      tern_migrate("spec/projects/no_alterations").should be_empty
    end

    it "creates definitions table on first run" do
      tern_migrate "spec/projects/new"
      @dev_db.tables.should include(:tern_definitions)
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