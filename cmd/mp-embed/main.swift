// mp-embed: NaturalLanguage sentence embeddings over stdin/stdout.
// Protocol: newline-delimited JSON.
//   Input:  {"text": "..."}
//   Output: {"embedding": [f32, ...]} or {"error": "..."}
//
// Runs as a persistent subprocess — Go keeps it alive across a batch.
// Build: swiftc -O cmd/mp-embed/main.swift -o bin/mp-embed -framework NaturalLanguage

import NaturalLanguage
import Foundation

struct Input: Decodable  { let text: String }
struct Output: Encodable {
    let embedding: [Float]?
    let error: String?
}

let encoder = JSONEncoder()
encoder.outputFormatting = []

guard let nlEmbedding = NLEmbedding.sentenceEmbedding(for: .english) else {
    fputs("mp-embed: NLEmbedding sentence model unavailable for English\n", stderr)
    exit(1)
}

while let line = readLine(strippingNewline: true) {
    guard !line.isEmpty else { continue }

    guard let data = line.data(using: .utf8),
          let input = try? JSONDecoder().decode(Input.self, from: data)
    else {
        let out = Output(embedding: nil, error: "parse error: \(line.prefix(80))")
        if let j = try? encoder.encode(out) { print(String(data: j, encoding: .utf8)!) }
        fflush(stdout)
        continue
    }

    // NLEmbedding.vector returns [Double]; downcast to Float to halve storage.
    if let vector = nlEmbedding.vector(for: input.text) {
        let floats = vector.map { Float($0) }
        let out = Output(embedding: floats, error: nil)
        if let j = try? encoder.encode(out) { print(String(data: j, encoding: .utf8)!) }
    } else {
        // Unknown token / empty string: return zero vector so callers can skip.
        let out = Output(embedding: nil, error: "no embedding for input")
        if let j = try? encoder.encode(out) { print(String(data: j, encoding: .utf8)!) }
    }
    fflush(stdout)
}
