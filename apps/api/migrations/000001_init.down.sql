-- Reverse dependency order: members -> households -> users.
-- schema_migrations belongs to golang-migrate and is intentionally left alone.
DROP TABLE IF EXISTS household_members;
DROP TABLE IF EXISTS households;
DROP TABLE IF EXISTS users;
