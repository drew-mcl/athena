# architecture

## how it works

you work on multiple features at once. each feature gets its own worktree, its own agent, its own task list. the merge queue keeps them ordered so nothing conflicts.

```mermaid
graph TD
    subgraph pm ["project management"]
        Linear["Linear"] --> Goals
        Jira["Jira"] --> Goals
        Goals["Goals<br/><i>strategic objectives</i>"]
    end

    Goals --> F1["Feature A"]
    Goals --> F2["Feature B"]
    Goals --> F3["Feature C"]

    subgraph parallel ["parallel development"]
        direction TB
        F1 --> W1["Worktree A<br/><code>../repo-feat-a</code>"]
        F2 --> W2["Worktree B<br/><code>../repo-feat-b</code>"]
        F3 --> W3["Worktree C<br/><code>../repo-feat-c</code>"]

        W1 --> A1["Agent A<br/><i>working</i>"]
        W2 --> A2["Agent B<br/><i>PR under review</i>"]
        W3 --> A3["Agent C<br/><i>done, merged</i>"]
    end

    subgraph queue ["merge queue"]
        direction TB
        Q1["#1 Feature C ✓"] --> Main["main"]
        Q2["#2 Feature B ⏳"] -.-> Main
        Q3["#3 Feature A 🔨"] -.-> Main
    end

    A1 -.-> Q3
    A2 -.-> Q2
    A3 --> Q1
```

## the parallel workflow

the key idea: you're never blocked. while one feature is under PR review, another agent is coding, and a third just merged. the queue handles ordering.

```mermaid
sequenceDiagram
    participant You
    participant Athena as ath CLI
    participant D as athenad
    participant A1 as Agent A
    participant A2 as Agent B
    participant GH as GitHub

    You->>Athena: ath spawn -f wi-a3f8.1
    Athena->>D: spawn(feature_id)
    D->>D: create worktree from queue head
    D->>D: set CLAUDE_CODE_TASK_LIST_ID
    D->>A1: launch claude code
    Note over A1: working on Feature A...

    You->>Athena: ath spawn -f wi-a3f8.2
    Athena->>D: spawn(feature_id)
    D->>D: create worktree from queue head
    D->>A2: launch claude code
    Note over A2: working on Feature B...

    Note over You: both agents working in parallel

    You->>Athena: ath tree
    Note over You: see both features + task progress

    A1->>GH: raises PR
    A1->>D: marks feature done
    D->>D: add to merge queue

    Note over GH: PR reviewed + approved

    GH->>D: PR merged (webhook/poll)
    D->>D: pop from queue, auto-rebase B
```

## feature lifecycle

every feature follows this path. athena manages each step.

```mermaid
stateDiagram-v2
    [*] --> Created: ath f new / linear / jira
    Created --> Spawned: ath spawn -f
    Spawned --> InProgress: agent starts working

    state InProgress {
        [*] --> Coding
        Coding --> TasksDone: all tasks complete
    }

    InProgress --> PROpen: agent raises PR
    PROpen --> InReview: gh/glab plugin
    InReview --> Queued: ath queue add
    Queued --> Merged: PR merged, popped from queue
    Merged --> [*]
```

## system components

```mermaid
graph LR
    subgraph cli ["CLI (ath)"]
        spawn["ath spawn"]
        tree["ath tree"]
        wt["ath wt"]
        queue["ath queue"]
        interactive["ath i"]
    end

    subgraph daemon ["athenad"]
        API["unix socket API"]
        Spawner["agent spawner"]
        QueueMgr["queue manager"]
        TaskWatch["task watcher"]
        PluginReg["plugin registry"]
    end

    subgraph plugins ["plugins"]
        GH["GitHub<br/><i>gh CLI</i>"]
        GL["GitLab<br/><i>glab CLI</i>"]
        LN["Linear<br/><i>GraphQL</i>"]
        JR["Jira<br/><i>REST API</i>"]
    end

    subgraph agents ["claude code agents"]
        Agent1["Agent 1<br/>worktree A"]
        Agent2["Agent 2<br/>worktree B"]
        Agent3["Agent N<br/>worktree N"]
    end

    subgraph storage ["storage"]
        SQLite["SQLite<br/><i>work items, agents,<br/>queue, worktrees</i>"]
        Tasks["~/.claude/tasks/<br/><i>native task lists</i>"]
    end

    cli --> API
    API --> Spawner
    API --> QueueMgr
    API --> TaskWatch
    API --> PluginReg

    PluginReg --> GH
    PluginReg --> GL
    PluginReg --> LN
    PluginReg --> JR

    Spawner --> Agent1
    Spawner --> Agent2
    Spawner --> Agent3

    Agent1 --> Tasks
    Agent2 --> Tasks
    Agent3 --> Tasks

    TaskWatch --> Tasks
    daemon --> SQLite
```

## what the agent gets

when you run `ath spawn -f <feature>`, the agent launches with:

```mermaid
graph TD
    subgraph injected ["injected into claude code"]
        SysPrompt["system prompt<br/><i>goal context + feature description<br/>+ available ath commands</i>"]
        TaskListID["CLAUDE_CODE_TASK_LIST_ID<br/><i>= feature work item ID</i>"]
        Worktree["working directory<br/><i>isolated git worktree</i>"]
    end

    subgraph agent ["claude code"]
        SysPrompt --> Work["does the work"]
        TaskListID --> TaskList["creates task list<br/><i>visible in ath tree</i>"]
        Worktree --> Git["isolated branch<br/><i>no conflicts with other features</i>"]
    end
```

## merge queue detail

the queue solves the "multiple features in flight" problem. new features always branch from queue head, so they stack cleanly.

```mermaid
graph LR
    Main["main"] --> Q1["Feature C<br/><i>merged ✓</i>"]
    Q1 --> Q2["Feature B<br/><i>PR open ⏳</i>"]
    Q2 --> Q3["Feature A<br/><i>agent working 🔨</i>"]

    Q3 -.-> |"new features<br/>branch from here"| New["Feature D<br/><i>next spawn</i>"]

    style Q1 fill:#2d5a2d,color:#fff
    style Q2 fill:#5a5a2d,color:#fff
    style Q3 fill:#2d2d5a,color:#fff
    style New fill:#3a3a3a,color:#fff,stroke-dasharray: 5 5
```

when you edit an earlier feature and run `ath queue bump`, dependents are automatically reconciled.

## data model

```mermaid
erDiagram
    GOAL ||--o{ FEATURE : contains
    FEATURE ||--o{ TASK : contains
    FEATURE ||--|| WORKTREE : "1:1"
    FEATURE ||--|| AGENT : "1:1"
    FEATURE ||--|| TASK_LIST : "1:1"
    FEATURE }o--|| MERGE_QUEUE : "position"

    GOAL {
        string id "wi-xxxx"
        string subject
        string ticket_id "ENG-123"
        string status
    }

    FEATURE {
        string id "wi-xxxx.N"
        string subject
        string parent_id "goal ID"
        string status
    }

    TASK {
        string id "wi-xxxx.N.M"
        string subject
        string parent_id "feature ID"
    }

    WORKTREE {
        string path
        string branch
    }

    AGENT {
        string id
        int pid
        string status
    }

    TASK_LIST {
        string id "= feature ID"
        string path "~/.claude/tasks/"
    }

    MERGE_QUEUE {
        int position
        string status
        string base_commit
    }
```
