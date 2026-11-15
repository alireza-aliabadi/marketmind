-- Bootstrap ownership namespaces, not business tables or runtime passwords.
DO $$
DECLARE
    schema_name TEXT;
    role_name TEXT;
BEGIN
    FOREACH schema_name IN ARRAY ARRAY[
        'identity', 'market_data', 'alerts', 'event_intelligence',
        'knowledge_graph', 'ai_orchestrator', 'quantitative'
    ] LOOP
        role_name := 'mm_' || schema_name;
        IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = role_name) THEN
            EXECUTE format(
                'CREATE ROLE %I NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS',
                role_name
            );
        ELSIF EXISTS (
            SELECT 1 FROM pg_roles WHERE rolname = role_name
            AND (rolcanlogin OR rolsuper OR rolcreatedb OR rolcreaterole OR rolreplication OR rolbypassrls)
        ) THEN
            RAISE EXCEPTION 'Existing role % conflicts with reserved ownership policy', role_name;
        END IF;

        IF EXISTS (
            SELECT 1 FROM pg_namespace WHERE nspname = schema_name
            AND pg_get_userbyid(nspowner) <> role_name
        ) THEN
            RAISE EXCEPTION 'Existing schema % has a different owner; migrate explicitly', schema_name;
        END IF;
        EXECUTE format('CREATE SCHEMA IF NOT EXISTS %I AUTHORIZATION %I', schema_name, role_name);
        EXECUTE format('REVOKE ALL ON SCHEMA %I FROM PUBLIC', schema_name);
    END LOOP;
END
$$;
