from app.control_plane.revision_gc import Revision, RevisionRetentionPolicy


def test_revision_gc_retains_latest_terminal_revisions_and_active_generation():
    revisions = [
        Revision(1, "ns/job-g1", "ready"),
        Revision(2, "ns/job-g2", "ready"),
        Revision(3, "ns/job-g3", "failed"),
        Revision(4, "ns/job-g4", "failed"),
        Revision(5, "ns/job-g5", "progressing"),
    ]
    garbage = RevisionRetentionPolicy(successful=1, failed=1).garbage_collect(revisions, active_generation=5)
    assert {item.provider_ref for item in garbage} == {"ns/job-g1", "ns/job-g3"}


def test_active_generation_is_never_garbage_collected_even_if_terminal():
    revisions = [Revision(7, "ns/job-g7", "ready")]
    assert RevisionRetentionPolicy(successful=0, failed=0).garbage_collect(revisions, 7) == []
