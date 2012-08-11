Tern Has Been Retired
=====================

This project has been retired. While it did solve the problem of database
objects like views and functions with dependencies on other database objects it
created other problems.

1. Dropping all definitions each run lost database level permissions.
2. Dropping and recreating all definitions caused longer service interruptions
   than only making necessary changes.
3. Alterations could not use database objects defined by definitions as they
   would not exist when the alteration was running.

With these problems in mind, it appears that it is better to use Rails or Sequel
style migrators and choose by hand what database objects must be created or
dropped.


Tern - The SQL Fan's Migrator
===============================

Tern is designed to simplify migrating database schemas with views, functions,
triggers, constraints and other objects where altering one may require dropping
and recreating others. For example, if view A selects from view B which selects
from table C, then altering  C could require five steps: drop A, drop B, alter
C, recreate B, and recreate A. Tern can be told to alter C and it will
automatically perform the other four steps.

Installation
============

    gem install tern

Requirements
============

Ruby 1.8.7+

Tern is built on top of [Sequel][1], so it can run on any database
[Sequel][1] supports. However, Tern relies on transactional DDL to keep the
database schema in a consistent state should something go wrong mid-migration.
[PostgreSQL][2] supports this. MySQL does not. Using Tern on a database
without transactional DDL is not recommended.

Creating a Tern Project
=======================

    tern new my_project

Edit config.yml and set up your database environments. Database environment
parameters are passed directly to Sequel.connect so it uses the same
[options][3].

    alterations:
      table: tern_alterations
      column: version
    definitions:
      table: tern_definitions
    environments:
      development:
        adapter: postgres
        database: my_project_development
      test:
        adapter: postgres
        database: my_project_test
      production:
        adapter: postgres
        database: my_project_production

Migration Types
===============

Tern divides potential database changes into alterations and definitions. An
alteration is something that cannot be reversed without potentially losing data.
For example, table creation is an alteration because dropping a table could
result in data loss. Alterations correspond directly with the migration style
popularized by Ruby on Rails. View creation is a definition, because it can be
dropped without possibility of data loss.

Both types of migrations have an extremely simple file format. They are simply
the create and drop SQL statements divided by a magic comment.

    ---- CREATE above / DROP below ----

Alterations
-----------

Alterations should be used to create, drop, and alter tables and any other
potentially irreversible migration.

    tern generate alteration create_people

or abbreviate

    tern g a create_people

This will create a numbered alteration in the alterations directory. Simply edit
the file and place the create code above the magic comment and the drop code
below it.

    CREATE TABLE people(
      id serial PRIMARY KEY,
      name varchar NOT NULL
    );

    ---- CREATE above / DROP below ----

    DROP TABLE people;

If this alteration is irreversible such as a drop table, simply delete the magic
comment.

    DROP TABLE widgets;

Definitions
-----------

Definitions should be used to specify the desired views, functions, triggers,
constraints, and other database objects that are reversible without possibility
of data loss.

    tern generate definition create_ultimate_answer

or abbreviate

    tern g d create_ultimate_answer


This will create the file create_ultimate_answer.sql in the definitions
directory. Add the create and drop commands around the magic comment.

    CREATE FUNCTION ultimate_answer() RETURNS integer AS $$
      SELECT 42;
    $$ LANGUAGE SQL;

    ---- CREATE above / DROP below ----

    DROP FUNCTION ultimate_answer();

Definitions need to be created in dropped in a particular order. This order is
defined in the file definitions/sequence.yml. Add this new definition to the
sequences file.

    default:
      - ultimate_answer.sql

Migrating
=========

    tern migrate

To run alterations to a specific version:

    tern migrate --alteration-version=42

To migrate a particular database environment:

    tern migrate --environment=test

How it Works
============

Tern in migrates in three steps.

1. Drop all definitions in the reverse order they were created
2. Run alterations
3. Create all definitions

Tern stores the drop command for definitions in the database when it is first
created. This allows it to totally reverse all definitions even without the
original definition files. To make changes to the definitions just change the
files and rerun tern migrate. Tern will drop all definitions it has previously
created and create your new definitions.

Multiple Definition Sequences
=============================

Definitions such as a check constraint on a table with many rows may be too
time-consuming to drop and recreate for every migration. Tern allows you to
define multiple definition sequences in the sequence.yml file.

    # default:
    #   - ultimate_answer.sql
    #   - my_first_view.sql
    #   - my_second_view.sql
    # expensive:
    #   - super_slow_check_constraint.sql

Only the default sequence will normally run. To migrate the expensive
definition sequence use the --definition-sequences option. Note that default
will not run unless specified when using this option.

    tern migrate --definition-sequences=expensive

Multiple sequences may be specified and they will be run in the order they are
listed.

    tern migrate --definition-sequences=expensive default
    
ERB
===

Alterations and definitions are run through ERB. Subtemplates may be 
included with the render_file method. Instance variables will be 
available in the subtemplates. 

definitions/render_file.sql

    <% @num_from_outer_template = 7 %>
    <%= render_file "definitions/rendered_function.sql" %>
    
definitions/rendered_function.sql

    CREATE FUNCTION rendered_function() RETURNS integer AS $$
      SELECT <%= @num_from_outer_template %>;
    $$ LANGUAGE SQL;

    ---- CREATE above / DROP below ----

    DROP FUNCTION rendered_function();

This will result in:

    CREATE FUNCTION rendered_function() RETURNS integer AS $$
      SELECT 7;
    $$ LANGUAGE SQL;

    ---- CREATE above / DROP below ----

    DROP FUNCTION rendered_function();
    
Version History
===============

* **0.7.1**
  * Print friendly error message when database error occurs instead of stack trace.
* **0.7.0**
  * Added ERB processing to SQL files
    
License
=======

Copyright (c) 2011 Jack Christensen, released under the MIT license

[1]: http://sequel.rubyforge.org/
[2]: http://www.postgresql.org/
[3]: http://sequel.rubyforge.org/rdoc/files/doc/opening_databases_rdoc.html
