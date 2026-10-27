-- 000001_init: identity and tenancy foundation (users, households, household_members).
--
-- Tenancy invariant: every table except `users` and `households` carries
-- `household_id`, directly (see migrations_test.go guard). `users` is global
-- identity; `households` is the tenant itself.
--
-- Table names are plural because `user` is a reserved word in Postgres.
-- Primary keys default to gen_random_uuid() (built in since PG13); the Go app
-- normally supplies UUIDv7 values.

CREATE TABLE users (
    id         uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    name       text        NOT NULL,
    email      text        CONSTRAINT users_email_not_empty CHECK (email <> ''),
    phone      text        CONSTRAINT users_phone_not_empty CHECK (phone <> ''), -- E.164, e.g. +6281234567890
    locale     text        NOT NULL DEFAULT 'id' CONSTRAINT users_locale_check CHECK (locale IN ('id', 'en')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX users_email_lower_key ON users (lower(email)) WHERE email IS NOT NULL;
CREATE UNIQUE INDEX users_phone_key ON users (phone) WHERE phone IS NOT NULL;

CREATE TABLE households (
    id         uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    name       text        NOT NULL,
    timezone   text        NOT NULL DEFAULT 'Asia/Jakarta'
                           CONSTRAINT households_timezone_check CHECK (timezone IN ('Asia/Jakarta', 'Asia/Makassar', 'Asia/Jayapura')),
    plan       text        NOT NULL DEFAULT 'free'
                           CONSTRAINT households_plan_check CHECK (plan IN ('free', 'plus')),
    created_by uuid        NOT NULL REFERENCES users (id),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX households_created_by_idx ON households (created_by);

CREATE TABLE household_members (
    household_id uuid        NOT NULL REFERENCES households (id) ON DELETE CASCADE,
    user_id      uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    role         text        NOT NULL
                             CONSTRAINT household_members_role_check CHECK (role IN ('owner', 'admin', 'member', 'viewer', 'helper')),
    status       text        NOT NULL DEFAULT 'active'
                             CONSTRAINT household_members_status_check CHECK (status IN ('active', 'invited', 'removed')),
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (household_id, user_id)
);

CREATE INDEX household_members_user_id_idx ON household_members (user_id);

-- Row level security: Supabase exposes the `public` schema through its Data API
-- (PostgREST), so every table must have RLS enabled. With RLS enabled and NO
-- policies, the `anon` and `authenticated` roles can read and write nothing,
-- while the Go server connects as the table owner and is unaffected (we
-- deliberately do not FORCE RLS). Add policies only if a table is ever meant
-- to be reachable through the Data API.
ALTER TABLE users             ENABLE ROW LEVEL SECURITY;
ALTER TABLE households        ENABLE ROW LEVEL SECURITY;
ALTER TABLE household_members ENABLE ROW LEVEL SECURITY;

-- golang-migrate creates schema_migrations before running this file; lock it
-- down the same way. IF EXISTS keeps the file valid with a custom migrations
-- table name.
ALTER TABLE IF EXISTS schema_migrations ENABLE ROW LEVEL SECURITY;
