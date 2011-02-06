DbSchemer
=========

DbSchemer is for managing database schemas with views, functions, triggers,
constraints and other objects that can be dependent on other objects. Altering
the type of one column may require multiple changes to dependent objects. For
example, when using multiple dependent views. If view A depends on view B which
depends on table C, then altering the type of a column on C could require
five steps: drop A, drop B, alter C, recreate B, and recreate A. DbSchemer can
be told to alter C and it will automatically perform the other four steps.

Installation
============

    gem install db_schemer

Requirements
============

DbSchemer is built on top of [Sequel][1], so it can run on any database
[Sequel][1] supports. However, DbSchemer relies on transactional DDL to keep the
database schema in a consistent state should something go wrong mid-migration.
[PostgreSQL][2] supports this. MySQL does not. Using DbSchemer on a database
without transactional DDL is not recommended.

License
=======

Copyright (c) 2011 Jack Christensen, released under the MIT license

[1]: http://sequel.rubyforge.org/
[2]: http://www.postgresql.org/