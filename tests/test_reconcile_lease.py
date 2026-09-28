from sqlalchemy.dialects import postgresql

from app.control_plane.reconcile_lease import ReconcileLeaseStore


class CapturingSession:
    def __init__(self):
        self.statement = None

    async def execute(self, statement):
        self.statement = statement
        return ScalarResult([])


class ScalarResult:
    def __init__(self, values):
        self.values = values

    def scalars(self):
        return self

    def all(self):
        return self.values


def test_sweep_candidates_include_current_generation_non_terminal_phase():
    session = CapturingSession()
    store = ReconcileLeaseStore(session)

    import asyncio
    assert asyncio.run(store.sweep_candidates()) == []

    sql = str(session.statement.compile(dialect=postgresql.dialect(), compile_kwargs={"literal_binds": True}))
    assert "observed_generation < control_plane_resources.generation" in sql
    assert "observed ->> 'phase'" in sql
    assert "NOT IN ('ready', 'failed')" in sql
    assert "lease_until IS NULL" in sql
