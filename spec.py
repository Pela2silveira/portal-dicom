import argparse
import os
from pathlib import Path

from dotenv import load_dotenv
from langchain_openai import ChatOpenAI


ROOT = Path(__file__).resolve().parent
ARTIFACTS_DIR = ROOT / "artifacts"
REQUIREMENTS_FILE = ROOT / "requirements.md"
DECISIONS_FILE = ROOT / "decisions.md"


def load_llm() -> ChatOpenAI:
    load_dotenv(ROOT / ".env")
    model = os.getenv("OPENAI_MODEL", "gpt-5.2")
    temperature = float(os.getenv("OPENAI_TEMPERATURE", "0"))
    return ChatOpenAI(model=model, temperature=temperature)


def read_file(path: Path) -> str:
    if not path.exists():
        return ""
    return path.read_text(encoding="utf-8")


def write_file(path: Path, content: str) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(content.strip() + "\n", encoding="utf-8")


def ask(llm: ChatOpenAI, system: str, human: str) -> str:
    response = llm.invoke(
        [
            {"role": "system", "content": system},
            {"role": "user", "content": human},
        ]
    )
    return response.content


def agent_architect(llm: ChatOpenAI, requirements: str, decisions: str) -> str:
    system = (
        "You are the lead architect for a PACS portal project. "
        "Produce concrete technical specs in Markdown. "
        "Favor explicit decisions, open questions, constraints, and acceptance criteria."
    )
    human = f"""
Project requirements:

{requirements}

Current human decisions:

{decisions or "No decisions have been recorded yet."}

Write a technical specification for implementation.
Requirements:
- Keep the architecture aligned with a DICOM aggregator + local cache + OHIF flow.
- Separate confirmed decisions from open questions.
- Include sections for system context, actors, data flow, security boundaries, retrieval/cache lifecycle, and implementation constraints.
- End with a short section titled "Open Questions Requiring Human Decision".
""".strip()
    return ask(llm, system, human)


def agent_debate(llm: ChatOpenAI, requirements: str, technical_spec: str, decisions: str) -> str:
    system = (
        "You are facilitating a short design review among three agents: Product, Security, and Operations. "
        "Return one Markdown document that clearly attributes each opinion."
    )
    human = f"""
Requirements:

{requirements}

Technical spec draft:

{technical_spec}

Human decisions so far:

{decisions or "No decisions recorded yet."}

Create a design discussion with this structure:
- Product Review
- Security Review
- Operations Review
- Decision Proposals

For each review:
- call out what is good,
- call out risks or ambiguity,
- list concrete decisions the human should make next.

In "Decision Proposals", produce a numbered list.
Each item must contain:
1. Decision name
2. Recommended option
3. Why this option is recommended
4. Alternatives to consider
""".strip()
    return ask(llm, system, human)


def agent_planner(llm: ChatOpenAI, technical_spec: str, debate: str, decisions: str) -> str:
    system = (
        "You are a technical planner. Produce an implementation plan that is sequenced, testable, and realistic."
    )
    human = f"""
Technical spec:

{technical_spec}

Agent discussion:

{debate}

Human decisions:

{decisions or "No decisions recorded yet."}

Create an implementation plan in Markdown.
Requirements:
- 5 to 8 milestones maximum.
- Each milestone must include goal, deliverables, dependencies, and exit criteria.
- Identify which milestones are blocked until the human resolves an open decision.
- Include a final section named "First Build Slice" with the smallest end-to-end slice to implement first.
""".strip()
    return ask(llm, system, human)


def agent_qa(llm: ChatOpenAI, technical_spec: str, plan: str, decisions: str) -> str:
    system = (
        "You are a QA and architecture reviewer. Produce a concise readiness checklist before coding starts."
    )
    human = f"""
Technical spec:

{technical_spec}

Implementation plan:

{plan}

Human decisions:

{decisions or "No decisions recorded yet."}

Write a QA checklist in Markdown.
Requirements:
- Group findings into Security, Functional, Integration, and Operability.
- Mark each line as Ready, Needs Decision, or Missing.
- End with a section called "Minimum Decisions Needed Before Coding".
""".strip()
    return ask(llm, system, human)


def ensure_requirements() -> str:
    requirements = read_file(REQUIREMENTS_FILE).strip()
    if not requirements:
        raise SystemExit("requirements.md is empty or missing.")
    return requirements


def run_architect(llm: ChatOpenAI) -> None:
    requirements = ensure_requirements()
    decisions = read_file(DECISIONS_FILE)
    output = agent_architect(llm, requirements, decisions)
    write_file(ARTIFACTS_DIR / "01_technical_spec.md", output)
    print("Wrote artifacts/01_technical_spec.md")


def run_debate(llm: ChatOpenAI) -> None:
    requirements = ensure_requirements()
    decisions = read_file(DECISIONS_FILE)
    technical_spec = read_file(ARTIFACTS_DIR / "01_technical_spec.md")
    if not technical_spec:
        raise SystemExit("Run the architect step first.")
    output = agent_debate(llm, requirements, technical_spec, decisions)
    write_file(ARTIFACTS_DIR / "02_agent_debate.md", output)
    print("Wrote artifacts/02_agent_debate.md")


def run_plan(llm: ChatOpenAI) -> None:
    decisions = read_file(DECISIONS_FILE)
    technical_spec = read_file(ARTIFACTS_DIR / "01_technical_spec.md")
    debate = read_file(ARTIFACTS_DIR / "02_agent_debate.md")
    if not technical_spec or not debate:
        raise SystemExit("Run the architect and debate steps first.")
    output = agent_planner(llm, technical_spec, debate, decisions)
    write_file(ARTIFACTS_DIR / "03_implementation_plan.md", output)
    print("Wrote artifacts/03_implementation_plan.md")


def run_qa(llm: ChatOpenAI) -> None:
    decisions = read_file(DECISIONS_FILE)
    technical_spec = read_file(ARTIFACTS_DIR / "01_technical_spec.md")
    plan = read_file(ARTIFACTS_DIR / "03_implementation_plan.md")
    if not technical_spec or not plan:
        raise SystemExit("Run the architect and plan steps first.")
    output = agent_qa(llm, technical_spec, plan, decisions)
    write_file(ARTIFACTS_DIR / "04_qa_checklist.md", output)
    print("Wrote artifacts/04_qa_checklist.md")


def run_all(llm: ChatOpenAI) -> None:
    run_architect(llm)
    run_debate(llm)
    run_plan(llm)
    run_qa(llm)


def main() -> None:
    parser = argparse.ArgumentParser(description="Spec-driven workflow for portal2.")
    parser.add_argument(
        "step",
        choices=["architect", "debate", "plan", "qa", "run"],
        help="Workflow step to execute.",
    )
    args = parser.parse_args()

    llm = load_llm()

    if args.step == "architect":
        run_architect(llm)
    elif args.step == "debate":
        run_debate(llm)
    elif args.step == "plan":
        run_plan(llm)
    elif args.step == "qa":
        run_qa(llm)
    else:
        run_all(llm)


if __name__ == "__main__":
    main()
