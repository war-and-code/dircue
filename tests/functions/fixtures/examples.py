def defective(divisor):
    return 10 / divisor

def guarded(divisor):
    if divisor == 0:
        return 0
    return 10 / divisor

def table(index):
    return [10, 20, 30][index]

def callback(operation, value):
    return operation(value)

def comprehension(values):
    return [value for value in values if value > 0]

def recover(value):
    try:
        return int(value)
    except ValueError:
        return 0

def outer(value):
    def inner(item):
        if item:
            return 1
        return 0
    return inner(value)
