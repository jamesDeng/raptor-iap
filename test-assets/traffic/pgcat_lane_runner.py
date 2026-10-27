"""Detached entry point; private bindings only, no request-supplied commands."""
import os
from pathlib import Path
import sys

from pgcat_lane_protocol import Journal, Refused, require
from pgcat_lane_supervisor import LaneSupervisor, read_json


def main():
    owner = None
    journal = None
    liveness_fd = None
    try:
        require(len(sys.argv) == 3)
        root = Path(sys.argv[1])
        liveness_fd = int(sys.argv[2])
        os.set_inheritable(liveness_fd, False)
        bindings = read_json(root/'bindings.json')
        journal = Journal(root)
        owner = LaneSupervisor(root, bindings, journal)
        owner.serve(liveness_fd)
        return 0 if journal.run_snapshot()['runState'] == 'finished' else 1
    except (Refused, OSError, ValueError, TypeError):
        if journal is not None: journal.cancel('incomplete')
        return 1
    finally:
        if owner is not None: owner.close()
        if liveness_fd is not None: os.close(liveness_fd)


if __name__ == '__main__':
    sys.exit(main())
