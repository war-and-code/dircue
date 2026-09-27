#!/usr/bin/env python3
"""Exercise whole-map availability across repeated valid declarations."""

import argparse
import json
import random
import subprocess
import tempfile
from pathlib import Path


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", type=Path, required=True)
    args = parser.parse_args()
    binary = args.binary.resolve()
    rng = random.Random(42)

    with tempfile.TemporaryDirectory(prefix="dircue-map-robustness-") as temporary:
        root = Path(temporary)
        workflow = root / ".github" / "workflows" / "check.yml"
        workflow.parent.mkdir(parents=True)
        for iteration in range(80):
            aliases = [rng.choice(("east", "west", "shared")) for _ in range(rng.randrange(1, 7))]
            (root / "main.tf").write_text(
                "\n".join(f'provider "aws" {{\n  alias = "{alias}"\n}}' for alias in aliases)
            )
            (root / "package.json").write_text(json.dumps({"name": f"app-{iteration}", "version": "1.0.0"}))
            workflow.write_text(
                "name: init & validate\n"
                "on:\n  push:\n  schedule:\n    - cron: '0 12 * * *'\n"
                "jobs:\n  check:\n    runs-on: ubuntu-latest\n"
                "    steps:\n      - run: echo ok && ls -d */\n"
            )
            (root / "k8s.yaml").write_text(
                "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n"
                f"  name: app-{iteration}\n---\napiVersion: v1\nkind: Service\n"
                f"metadata:\n  name: app-{iteration}\n"
            )
            result = subprocess.run(
                [str(binary), "map", "--json", "--source", "directory", str(root)],
                capture_output=True, text=True, timeout=10,
            )
            if result.returncode:
                raise AssertionError(f"variant {iteration} lost the map: {result.stderr[:500]}")
            document = json.loads(result.stdout)
            if document["kind"] != "map" or not document["coverage"]:
                raise AssertionError(f"variant {iteration} returned an incomplete document")

        # Variants 80-89: doc-named config files.
        #
        # A prior bug caused map to abort whenever a repo contained workflow or
        # Kubernetes files whose basenames started with readme, changelog, or
        # contributing (e.g. .github/workflows/changelog.yml).  isDocumentationPath
        # treated them as prose documentation, so the validator rejected deployable
        # evidence that pointed to them.  These variants exercise the full grid of
        # affected patterns so a regression would be caught immediately.
        doc_named_workflows = [
            "changelog.yml",
            "changelog.yaml",
            "readme.yml",
            "readme.yaml",
            "contributing-check.yaml",
        ]
        doc_named_k8s = [
            "changelog-service.yaml",
            "readme-init.yaml",
            "contributing-app.yaml",
            "changelog-deployment.yaml",
            "readme-configmap.yaml",
        ]
        wf_dir = root / ".github" / "workflows"
        wf_dir.mkdir(parents=True, exist_ok=True)
        deploy_dir = root / "deploy"
        deploy_dir.mkdir(parents=True, exist_ok=True)

        for idx, (wf_name, k8s_name) in enumerate(
            zip(doc_named_workflows, doc_named_k8s)
        ):
            iteration = 80 + idx
            # Write the doc-named workflow file.
            (wf_dir / wf_name).write_text(
                f"name: {wf_name.rsplit('.', 1)[0]}\n"
                "on:\n  pull_request:\n    branches: [main]\n"
                "jobs:\n  check:\n    runs-on: ubuntu-latest\n"
                "    steps:\n      - uses: actions/checkout@v4\n"
            )
            # Write a doc-named Kubernetes manifest.
            svc = k8s_name.rsplit('.', 1)[0]
            (deploy_dir / k8s_name).write_text(
                "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n"
                f"  name: {svc}\n"
                "spec:\n  replicas: 1\n  selector:\n    matchLabels:\n"
                f"      app: {svc}\n"
                "  template:\n    metadata:\n      labels:\n"
                f"        app: {svc}\n"
                "    spec:\n      containers:\n"
                f"      - name: {svc}\n        image: {svc}:latest\n"
            )
            # Also maintain a minimal go.mod so the map finds a component.
            (root / "go.mod").write_text(
                f"module example.com/robustness-{iteration}\n\ngo 1.21\n"
            )
            (root / "main.go").write_text("package main\nfunc main() {}\n")
            result = subprocess.run(
                [str(binary), "map", "--json", "--source", "directory", str(root)],
                capture_output=True, text=True, timeout=10,
            )
            if result.returncode:
                raise AssertionError(
                    f"variant {iteration} (doc-named config: {wf_name}, {k8s_name})"
                    f" lost the map: {result.stderr[:500]}"
                )
            document = json.loads(result.stdout)
            if document["kind"] != "map" or not document["coverage"]:
                raise AssertionError(
                    f"variant {iteration} returned an incomplete document"
                )

    print(json.dumps({"gate": "whole-map-availability", "variants": 90, "status": "passed"}))


if __name__ == "__main__":
    main()
