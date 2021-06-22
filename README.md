Havi ("High One" in Old Norse) is a scheduling level for various OTA tools

# Architecture

All components are deployed via GitOps (ArgoCD), each having its own Application set.
Havi is the scheduler:
* Havi reads cincinnati-graph-data, parses channels to get a list of tracked releases
* Havi starts 4 Tekton pipelines, described below
* First pipeline is Huginn. Huginn parses telemetry data and prepares cached data for reports
* Second pipeline is Gersemi. For each tracked release Gersemi task is building a version-specific report
* Third pipelene is Eir, which builds status and triage reports.
* Final pipeline is Muninn, which tracks erratas and ensures candidate -> fast -> stable / eus promotion PRs are created / approved.
* Team in notified in Slack via Hermod component
* When one of the edges is showing may need to be pulled, Havi is invoking Svidur and a PR to tombstone a particular version is created.
* All components create artifacts in form of HTML reports. These reports are rendered by nginx deployment.

# Requirements

The cluster must have Tekton (Openshift pipelines) installed to make use of scheduling / metrics provided by Tekton.
Tasks would write data to disk, so the cluster must provide a PV to store results.
