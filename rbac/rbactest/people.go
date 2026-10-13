package rbactest

import (
	"github.com/flanksource/duty/context"
	"github.com/flanksource/incident-commander/vars"
)

// SetupVerifiedPeople gives the authorization suites explicit, verified Kratos email fixtures.
// It is only for disposable test databases; verification security tests must not use it.
func SetupVerifiedPeople(ctx context.Context) error {
	vars.AuthMode = "kratos"
	return ctx.DB().Exec(`
CREATE TABLE identity_verifiable_addresses (
    identity_id uuid NOT NULL, value text NOT NULL, via text NOT NULL, verified boolean NOT NULL,
    PRIMARY KEY (identity_id, value)
);
INSERT INTO identity_verifiable_addresses
    SELECT id, email, 'email', true FROM people WHERE email IS NOT NULL;
CREATE FUNCTION rbactest_verify_person() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    DELETE FROM identity_verifiable_addresses WHERE identity_id = NEW.id;
    IF NEW.email IS NOT NULL THEN
        INSERT INTO identity_verifiable_addresses VALUES (NEW.id, NEW.email, 'email', true);
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER rbactest_verify_person AFTER INSERT OR UPDATE OF email ON people
    FOR EACH ROW EXECUTE FUNCTION rbactest_verify_person();
`).Error
}
