# Agent skills releases

Publish the complete `agent-skills/` folder with a Git tag named `skills-vMAJOR.MINOR.PATCH`. A request selects a published stable tag and stores the exact commit SHA. The highest stable semantic version is the default; older releases remain selectable. Do not move published tags.

Users choose whether a version change interrupts the current execution or waits for its next approval/review pause. Conversation checkpoints and progress remain associated with the same request. Completing before that pause does not restart the request.

The local Go slice uses synthetic execution. These conventions do not establish a deployed skills release or a working PgCat replacement procedure.
