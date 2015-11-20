notify = require '../notify'

describe "notify", ->
  beforeEach -> @notifier = notify "Test"

  it "is a function", -> expect(notify.call).toBeDefined()

  it "returns an object with notification creating methods", ->
    expect(@notifier.addInfo).toBeDefined()
    expect(@notifier.addSuccess).toBeDefined()
    expect(@notifier.addError).toBeDefined()
    expect(@notifier.addWarning).toBeDefined()

  it "accepts a string parameter as the title for notifications", ->
    {message} = @notifier.addSuccess "Sucess message"
    expect(message).toBe "Test"

  describe "methods on notifier object", ->
    it "accepts an options object", ->
      message = @notifier.addSuccess "Success message", dismissable: true
      expect(message.options.dismissable).toBe true
