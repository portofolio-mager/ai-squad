

const wait = (ms)=>new Promise((resolve)=> setTimeout( resolve, ms));

describe('Example Test', () => {
  it('should pass', () => {
    expect(true).toBe(true)
  })

  it('should render a component', () => {
    expect(1 + 1).toBe(2)
  })

  it('should fail to demonstrate error handling', async () => {
    await wait(1000);
    expect(1 + 1).toBe(3) // This will fail
  })
})