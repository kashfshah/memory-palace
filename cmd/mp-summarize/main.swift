// mp-summarize: on-device summarization via FoundationModels (macOS 26+).
// Protocol: newline-delimited JSON.
//   Input:  {"text": "..."}
//   Output: {"summary": "..."} or {"error": "..."}
//
// Runs as a persistent subprocess — Go keeps it alive across a batch.
// Build: swiftc -O cmd/mp-summarize/main.swift -o bin/mp-summarize

import FoundationModels
import Foundation

struct Input:  Decodable { let text: String }
struct Output: Encodable { let summary: String?; let error: String? }

let encoder = JSONEncoder()

// Check availability once up front.
guard case .available = SystemLanguageModel.default.availability else {
    fputs("mp-summarize: FoundationModels not available on this device\n", stderr)
    exit(1)
}

// One session per process — reused across all inputs for efficiency.
let session = LanguageModelSession()

func emit(_ out: Output) {
    if let j = try? encoder.encode(out) { print(String(data: j, encoding: .utf8)!) }
    fflush(stdout)
}

// Bridge async session.respond to the synchronous stdin loop.
// Each line blocks until the model responds before reading the next.
Task {
    while let line = readLine(strippingNewline: true) {
        guard !line.isEmpty else { continue }

        guard let data = line.data(using: .utf8),
              let input = try? JSONDecoder().decode(Input.self, from: data)
        else {
            emit(Output(summary: nil, error: "parse error"))
            continue
        }

        let prompt = """
        Summarize the following in 1–2 sentences. Return only the summary, no preamble.

        \(input.text.prefix(2000))
        """

        do {
            let response = try await session.respond(to: prompt)
            emit(Output(summary: response.content, error: nil))
        } catch {
            emit(Output(summary: nil, error: error.localizedDescription))
        }
    }
    exit(0)
}

dispatchMain()
