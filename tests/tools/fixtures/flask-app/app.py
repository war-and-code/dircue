"""Minimal Flask application fixture for OWASP Noir and SARIF oracle testing."""
from flask import Flask, request, jsonify

app = Flask(__name__)

items = {}


@app.route("/items", methods=["GET"])
def list_items():
    """List all items."""
    return jsonify(list(items.values()))


@app.route("/items", methods=["POST"])
def create_item():
    """Create a new item."""
    data = request.get_json()
    item_id = len(items) + 1
    items[item_id] = {"id": item_id, "name": data.get("name", "")}
    return jsonify(items[item_id]), 201


@app.route("/items/<int:item_id>", methods=["GET"])
def get_item(item_id):
    """Get a specific item by ID."""
    item = items.get(item_id)
    if item is None:
        return jsonify({"error": "not found"}), 404
    return jsonify(item)


@app.route("/items/<int:item_id>", methods=["DELETE"])
def delete_item(item_id):
    """Delete an item by ID."""
    items.pop(item_id, None)
    return "", 204


@app.route("/health", methods=["GET"])
def health():
    """Health check endpoint."""
    return jsonify({"status": "ok"})


if __name__ == "__main__":
    app.run(debug=False)
