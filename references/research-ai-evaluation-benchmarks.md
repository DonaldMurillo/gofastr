# AI Coding / Programming Capability Evaluation Harnesses

A comprehensive list of frameworks and benchmarks designed to evaluate AI models on software engineering, code generation, debugging, and programming tasks.

---

## 1. HumanEval
- **URL**: https://github.com/openai/human-eval
- **Maintained by**: OpenAI
- **Description**: The original and most widely-cited code generation benchmark. Presents 164 hand-crafted Python programming problems where the model must complete a function body given a docstring description. Evaluates functional correctness via unit test pass rates (pass@k metrics).
- **Key Features**:
  - 164 Python programming problems with unit tests
  - Introduced pass@k (pass@1, pass@10, pass@100) evaluation metric
  - Gold standard benchmark used by virtually every code LLM paper
  - Simple to run; evaluates functional correctness by executing generated code
  - Basis for many derivative benchmarks (EvalPlus, MultiPL-E, etc.)

---

## 2. MBPP (Mostly Basic Python Problems)
- **URL**: https://github.com/google-research/google-research/tree/master/mbpp
- **Maintained by**: Google Research
- **Description**: A dataset of 974 crowd-sourced Python programming problems designed to be solvable by entry-level programmers. Each problem includes a task description, code solution, and test cases.
- **Key Features**:
  - 974 Python problems across varying difficulty levels
  - Human-written problem descriptions and reference solutions
  - Unit test-based evaluation
  - Often used alongside HumanEval for comprehensive assessment
  - Available in sanitized (738 problems) and full versions

---

## 3. SWE-bench
- **URL**: https://github.com/SWE-bench/SWE-bench
- **Maintained by**: Princeton NLP (Carlos Jimenez, John Yang, et al.)
- **Description**: Evaluates AI systems on their ability to resolve real-world GitHub issues. Given a codebase and an issue description, the model must generate a patch that fixes the described problem. ICLR 2024 Oral.
- **Key Features**:
  - Real-world GitHub issues from popular Python repositories (Django, scikit-learn, Flask, etc.)
  - Docker-based containerized evaluation for reproducibility
  - Multiple tiers: Full (2294), Lite (300), Verified (500 human-verified)
  - SWE-bench Multimodal variant for visual software domains
  - Companion tools: SWE-agent, SWE-smith for training data generation

---

## 4. CodeContests
- **URL**: https://github.com/google-deepmind/code_contests
- **Maintained by**: Google DeepMind
- **Description**: A competitive programming dataset used to train AlphaCode. Contains problems from Aizu, AtCoder, CodeChef, Codeforces, and HackerEarth, with paired inputs/outputs and both correct/incorrect human solutions.
- **Key Features**:
  - ~13,000+ competitive programming problems from 5 contest platforms
  - Includes correct and incorrect human solutions for training
  - Sandboxed execution environment for safe code evaluation
  - Used to develop AlphaCode (achieved top 54.3% on Codeforces)

---

## 5. EvalPlus (HumanEval+ / MBPP+)
- **URL**: https://github.com/evalplus/evalplus
- **Maintained by**: UIUC (Jiawei Liu, Lingming Zhang et al.)
- **Description**: Augments HumanEval and MBPP with massively more test cases. HumanEval+ has 80x more tests; MBPP+ has 35x more tests. Also includes EvalPerf for evaluating code efficiency. NeurIPS 2023.
- **Key Features**:
  - HumanEval+: 80x more tests than original (eliminates false positives)
  - MBPP+: 35x more tests than original MBPP
  - EvalPerf: evaluates code efficiency, not just correctness
  - 10+ inference backends (vLLM, HF, OpenAI, Anthropic, Google, Ollama, etc.)
  - Used by Meta Llama, Qwen, DeepSeek, Snowflake, StarCoder2 teams

---

## 6. BigCodeBench
- **URL**: https://github.com/bigcode-project/bigcodebench
- **Maintained by**: BigCode Community / Monash University
- **Description**: 1,140 software-engineering-oriented tasks evaluating practical code generation with diverse function calls and complex instructions. Tests real-world programming involving library APIs and tool usage.
- **Key Features**:
  - 1,140 practical programming tasks with diverse function calls
  - Complete (docstring-based) and Instruct (instruction-tuned) splits
  - BigCodeBench-Hard: 148-task harder subset
  - Remote evaluation API (no local GPU needed for execution)
  - Pre-generated samples for 163+ models
  - Trusted by DeepSeek, Qwen, Meta AI, Amazon AWS AI, etc.

---

## 7. LiveCodeBench
- **URL**: https://github.com/LiveCodeBench/LiveCodeBench
- **Maintained by**: UC Berkeley / UIUC (Naman Jain et al.)
- **Description**: Continuously collects new competitive programming problems from LeetCode, AtCoder, and Codeforces. Mitigates data contamination. Also evaluates self-repair, code execution, and test output prediction.
- **Key Features**:
  - Continuously updated (1,055+ problems across 6 releases)
  - Sources from LeetCode, AtCoder, Codeforces
  - Contamination-free: evaluate on problems released after model training
  - Scenarios: Code Generation, Self-Repair, Test Output Prediction, Code Execution
  - Time-windowed evaluation to detect contamination

---

## 8. MultiPL-E
- **URL**: https://github.com/nuprl/MultiPL-E
- **Maintained by**: Northeastern University PL Lab (Arjun Guha et al.)
- **Description**: Translates unit test-driven code generation benchmarks to 18+ programming languages beyond Python. Essential for evaluating multi-language code generation.
- **Key Features**:
  - HumanEval and MBPP translated to 18+ languages (JS, TS, C++, Go, Rust, Java, PHP, Ruby, etc.)
  - Automated test translation via program analysis
  - Docker/Podman-based safe execution
  - Extensible framework for adding new languages
  - Published at IEEE TSE

---

## 9. CodeXGLUE
- **URL**: https://github.com/microsoft/CodeXGLUE
- **Maintained by**: Microsoft Research Asia
- **Description**: Comprehensive benchmark covering 10 code intelligence tasks across 14 datasets. Evaluates code search, clone detection, defect detection, completion, translation, repair, summarization, and more.
- **Key Features**:
  - 10 diversified tasks across 14 datasets
  - Covers: clone detection, defect detection, code completion, translation, search, repair, summarization
  - Baseline models: CodeBERT, CodeGPT, Encoder-Decoder
  - Official leaderboard with test set evaluation
  - The "GLUE for code" - broadest task coverage

---

## 10. OSWorld
- **URL**: https://github.com/xlang-ai/OSWorld
- **Maintained by**: XLang Lab / HKU / Salesforce
- **Description**: Benchmark for multimodal agents on open-ended tasks in real desktop environments. Agents interact with actual OS GUIs to complete tasks involving web browsing, document editing, file management, and coding.
- **Key Features**:
  - Real VM-based environments (Ubuntu, Windows, macOS)
  - 369+ tasks across office, web, coding, and professional domains
  - Supports VMware, VirtualBox, Docker, AWS execution
  - Evaluates end-to-end agent capabilities (not just code generation)
  - OSWorld-Verified: community-reviewed improved version

---

## Summary Comparison

| Benchmark | Focus | Languages | Scale | Key Strength |
|-----------|-------|-----------|-------|-------------|
| HumanEval | Function code generation | Python | 164 | Gold standard |
| MBPP | Basic programming | Python | 974 | Large scale |
| SWE-bench | Real GitHub issue fixes | Python | 2,294 | Most realistic SE eval |
| CodeContests | Competitive programming | Multi | 13,000+ | Competition-level difficulty |
| EvalPlus | Rigorous test augmentation | Python | 164+974 | Eliminates false positives |
| BigCodeBench | Practical API-based tasks | Python | 1,140 | Real-world tool usage |
| LiveCodeBench | Contamination-free eval | Multi | 1,055+ | Continuously updated |
| MultiPL-E | Multi-language generation | 19+ langs | 164x19+ | Cross-language coverage |
| CodeXGLUE | Full code intelligence | Multi | 14 datasets | Broadest task coverage |
| OSWorld | Desktop agent tasks | Multi | 369+ | End-to-end agent eval |

## Additional Notable Mentions

- **APPS** (https://github.com/hendrycks/apps) - 10,000 competitive programming problems across 5 difficulty levels
- **HumanEvalPack** (Meta) - Extends HumanEval to 6 languages with code repair, test generation, summarization
- **DS-1000** - 1,000 data science problems with pandas/numpy/scipy/matplotlib
- **CRUXEval** - Code reasoning benchmark for input/output prediction
- **Aider Leaderboard** (https://aider.chat/docs/leaderboards/) - Ranks LLMs on practical code editing in git repos
